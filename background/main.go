// Генератор унікального фонового відео для YouTube.
//
// Логіка одного запуску:
//  1. Обираємо N випадкових тематичних запитів із великого пулу.
//  2. Качаємо з Pexels рівно N НОВИХ кліпів — таких, ID яких ще НІКОЛИ
//     не зустрічалися в clip_library.json. Реєстр вічний, тому два запуски
//     фізично не можуть використати один і той самий вихідник.
//  3. Кожен вихідник нормалізуємо до 1920x1080@30 і робимо безшовний луп.
//  4. Ріжемо таймлайн на сегменти. Кожен сегмент бере випадковий шматок
//     випадкового кліпу і отримує СВІЙ набір трансформацій (zoom, повільний
//     панорамний дрейф, hue, eq, тонування, шум, різкість, швидкість, дзеркало).
//  5. Склеюємо з кросфейдами до потрібного хронометражу.
//
// Потрібні ffmpeg і ffprobe у PATH.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ==================== НАЛАШТУВАННЯ ====================

const (
	defaultAPIKey = "YnreqH9WjTQr5TFYfUD5nUKN5brsGMYWYZFKv7YFeOtXdBI5TzeV4K0d"

	libraryFile = "clip_library.json" // вічний реєстр використаних Pexels ID
	workDir     = "work"              // проміжні файли

	outWidth  = 1920
	outHeight = 1080
	outFPS    = 30

	minSourceDur  = 12.0 // коротші кліпи не беремо — надто помітний луп
	prepCapSec    = 240  // скільки секунд кожного вихідника нормалізувати
	maxPagesToTry = 6    // скільки сторінок видачі Pexels перебрати на запит
	gopSize       = 60   // фіксований GOP: рівні межі для склейки без перекодування

	// Поріг середньої яскравості (YAVG, 0..255) свіжозавантаженого кліпу.
	// Нижче — надто темний, такий відсіюємо: фон має бути добре видно. Ніч
	// прибрана з пулу, тож поріг тримаємо високим — денний пейзаж легко дає
	// 90+, а все сутінкове/затемнене відпадає. Підняли 60->82: на 60
	// пролазили туманний ліс і нічне місто (яскраві вогні тягнуть середню
	// люму вгору, хоча великі ділянки темні) — у зібраному відео ~40%
	// виходило темним.
	minMeanLuma = 82.0
)

// Пул тем. На кожен запуск береться cfg.Clips випадкових запитів — по одному
// кліпу з кожного, щоб у відео були різні сюжети, а не 8 варіацій лісу.
// Пул тем. Свідомо ТІЛЬКИ широкі плани — пейзажі, аеро/дрон, timelapse —
// без макро й крупних планів (об'єкт упритул виглядає як стокова заставка,
// а не фон). І ТІЛЬКИ денне/світле: ніч прибрана взагалі (за проханням), а
// поріг яскравості нижче відсіює будь-що випадково затемнене.
var queryPool = []string{
	"misty forest valley drone", "ocean waves aerial wide", "mountains sunrise timelapse",
	"calm lake reflection wide", "turquoise lake mountains aerial", "snowy mountain range aerial",
	"desert dunes aerial wide", "waterfall canyon aerial", "clouds timelapse over valley",
	"autumn forest hills aerial", "sunny coastline turquoise water aerial", "river valley aerial view",
	"pine forest fog aerial", "green rolling hills wind", "fog rolling over mountains",
	"blue sky clouds timelapse", "lavender fields sunset wide", "glacier landscape aerial",
	"tropical coastline aerial", "wheat fields golden hour wide", "grand canyon aerial view",
	"mossy forest stream wide", "sea cliffs waves aerial", "rice terraces aerial",
	"green valley river aerial", "rainforest valley mist aerial", "frozen lake mountains drone",
	"alpine meadow mountains wide", "sunny ocean horizon aerial", "sunbeams over forest hills",
	"iceberg ocean aerial", "sunny palm coastline aerial", "mountain river valley aerial",
	"clouds above mountain peaks", "countryside fields aerial", "salt flats reflection wide",
	"autumn valley golden aerial", "spring meadow flowers aerial", "coastal cliffs daytime aerial",
	"green terraced hills aerial", "savanna plains aerial", "fjord landscape aerial",
}

// Тільки м'які кросфейди. Напрямні wipe/slide (smoothleft/right/up/down,
// circleopen) на ambient-фоні читаються як різкий стрибок картинки вбік —
// саме тому вони прибрані: лишилося тільки плавне перетікання.
var transitions = []string{"fade", "dissolve"}

var cfg struct {
	APIKey     string
	Clips      int
	Minutes    float64
	SegLen     float64
	Crossfade  float64
	Out        string
	Workers    int
	MergeBatch int
	Transition string
	Encoder    string
	Seed       int64
	Keep       bool
}

func init() {
	flag.StringVar(&cfg.APIKey, "key", "", "Pexels API ключ (або змінна оточення PEXELS_API_KEY)")
	flag.IntVar(&cfg.Clips, "clips", 8, "скільки НОВИХ кліпів качати за запуск")
	flag.Float64Var(&cfg.Minutes, "minutes", 60, "тривалість фінального відео у хвилинах")
	flag.Float64Var(&cfg.SegLen, "seg", 90, "бажана довжина одного сегмента, сек")
	flag.Float64Var(&cfg.Crossfade, "crossfade", 1.5, "довжина переходу між сегментами, сек")
	flag.StringVar(&cfg.Out, "out", "", "ім'я вихідного файлу (типово background_<дата>.mp4)")
	flag.IntVar(&cfg.Workers, "workers", 0, "скільки шматків рендерити паралельно (0 = авто)")
	flag.IntVar(&cfg.MergeBatch, "mergebatch", 16, "розмір партії для режиму -transition merge")
	flag.StringVar(&cfg.Transition, "transition", "xfade", "xfade (1 прохід кодування, швидко) або merge (класичний ланцюжок, 3 проходи)")
	flag.StringVar(&cfg.Encoder, "encoder", "auto", "auto | nvenc | qsv | amf | x264")
	flag.Int64Var(&cfg.Seed, "seed", 0, "зерно генератора (0 = випадкове)")
	flag.BoolVar(&cfg.Keep, "keep", false, "не видаляти проміжні файли з work/")
}

// ==================== PEXELS API ====================

type PexelsVideoFile struct {
	Quality  string `json:"quality"`
	FileType string `json:"file_type"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Link     string `json:"link"`
}

type PexelsVideo struct {
	ID       int               `json:"id"`
	Width    int               `json:"width"`
	Height   int               `json:"height"`
	Duration int               `json:"duration"`
	Files    []PexelsVideoFile `json:"video_files"`
}

type PexelsSearchResponse struct {
	TotalResults int           `json:"total_results"`
	Videos       []PexelsVideo `json:"videos"`
}

// ==================== ВІЧНИЙ РЕЄСТР ВИКОРИСТАНИХ КЛІПІВ ====================

// UsedClip — запис про кліп, який уже колись потрапив у відео. Файл на диску
// може бути давно видалений; важливий тільки ID, щоб цей Pexels-кліп більше
// ніколи не повторився в наступних запусках.
type UsedClip struct {
	ID       int       `json:"id"`
	Query    string    `json:"query"`
	Duration float64   `json:"duration"`
	UsedAt   time.Time `json:"used_at"`
	Run      int       `json:"run"`
}

// legacyClip — формат старої версії скрипта, читаємо лише для міграції.
type legacyClip struct {
	Query    string  `json:"query"`
	ID       int     `json:"id"`
	Path     string  `json:"path"`
	Duration float64 `json:"duration"`
}

type Library struct {
	Runs   int          `json:"runs"`
	Used   []UsedClip   `json:"used"`
	Legacy []legacyClip `json:"clips,omitempty"`

	index map[int]bool
}

func loadLibrary() *Library {
	lib := &Library{index: map[int]bool{}}

	data, err := os.ReadFile(libraryFile)
	if err != nil {
		return lib // перший запуск — це нормально
	}
	if err := json.Unmarshal(data, lib); err != nil {
		fmt.Printf("⚠️  %s пошкоджений (%v). Роблю резервну копію й починаю з нуля.\n", libraryFile, err)
		_ = os.Rename(libraryFile, libraryFile+".broken")
		return &Library{index: map[int]bool{}}
	}

	// Міграція зі старого формату: переносимо ID у вічний реєстр.
	if len(lib.Legacy) > 0 {
		for _, c := range lib.Legacy {
			lib.Used = append(lib.Used, UsedClip{ID: c.ID, Query: c.Query, Duration: c.Duration})
		}
		fmt.Printf("🔄 Перенесено %d ID зі старого формату бібліотеки.\n", len(lib.Legacy))
		lib.Legacy = nil
	}

	lib.index = make(map[int]bool, len(lib.Used))
	for _, c := range lib.Used {
		lib.index[c.ID] = true
	}
	return lib
}

func (lib *Library) save() error {
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(libraryFile, data, 0644)
}

func (lib *Library) seen(id int) bool { return lib.index[id] }

// markUsed реєструє кліп і одразу пише файл на диск. Робимо це в момент
// СКАЧУВАННЯ, а не в кінці: якщо запуск обірветься посередині, ці кліпи все
// одно вважаються витраченими й не спливуть у наступному відео.
func (lib *Library) markUsed(c UsedClip) {
	if lib.index == nil {
		lib.index = map[int]bool{}
	}
	lib.index[c.ID] = true
	lib.Used = append(lib.Used, c)
	if err := lib.save(); err != nil {
		fmt.Printf("⚠️  Не вдалося зберегти %s: %v\n", libraryFile, err)
	}
}

// ==================== ВИБІР КОДЕКА ====================

type Encoder struct {
	Name   string
	Args   []string
	IsCPU  bool
	Chosen bool
}

var encoderCandidates = []Encoder{
	{Name: "h264_nvenc", Args: []string{"-c:v", "h264_nvenc", "-preset", "p5", "-rc", "constqp", "-qp", "23", "-profile:v", "high"}},
	{Name: "h264_qsv", Args: []string{"-c:v", "h264_qsv", "-preset", "veryfast", "-global_quality", "23"}},
	{Name: "h264_amf", Args: []string{"-c:v", "h264_amf", "-quality", "balanced", "-rc", "cqp", "-qp_i", "23", "-qp_p", "23"}},
	{Name: "libx264", Args: []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "21",
		"-maxrate", "14M", "-bufsize", "28M", "-sc_threshold", "0"}, IsCPU: true},
}

// cfFrames — довжина переходу в КАДРАХ. Весь таймлайн рахується в кадрах, а не
// в секундах: шматок тривалістю, не кратною 1/fps, дає на шві зайвий кадр із
// тим самим PTS, і склейка ловить "non monotonically increasing dts".
var cfFrames int

func framesToSec(n int) float64 { return float64(n) / outFPS }

// encodeArgs дає однакові параметри для КОЖНОГО шматка — інакше склейка без
// перекодування (concat -c copy) не працює.
func (e Encoder) encodeArgs() []string {
	args := append([]string{}, e.Args...)
	if e.IsCPU && cfg.Workers > 1 {
		// Кілька паралельних x264 навперебій хапають усі ядра й заважають
		// одне одному. Ділимо ядра порівну.
		t := runtime.NumCPU() / cfg.Workers
		if t < 1 {
			t = 1
		}
		args = append(args, "-threads", strconv.Itoa(t))
	}
	return append(args, "-g", strconv.Itoa(gopSize), "-keyint_min", strconv.Itoa(gopSize), "-pix_fmt", "yuv420p")
}

func pickEncoder() Encoder {
	x264 := encoderCandidates[len(encoderCandidates)-1]

	var candidates []Encoder
	switch cfg.Encoder {
	case "auto":
		candidates = encoderCandidates
	case "x264":
		return x264
	default:
		for _, e := range encoderCandidates {
			if strings.Contains(e.Name, cfg.Encoder) {
				candidates = []Encoder{e}
			}
		}
		if candidates == nil {
			fmt.Printf("⚠️  Невідомий кодер %q, беру libx264.\n", cfg.Encoder)
			return x264
		}
	}

	for _, e := range candidates {
		args := append([]string{"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30:duration=0.2"}, e.encodeArgs()...)
		args = append(args, "-f", "null", "-")
		if err := runFFmpeg(args...); err == nil {
			return e
		}
	}
	return x264
}

// ==================== ЗАПУСК FFMPEG ====================

func runFFmpeg(args ...string) error {
	full := append([]string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y"}, args...)
	cmd := exec.Command("ffmpeg", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w\n%s", err, tailLines(stderr.String(), 12))
	}
	return nil
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// meanLuma повертає середню яскравість (YAVG, 0..255) по 12 кадрах,
// РІВНОМІРНО розкиданих по всій тривалості кліпу (duration, сек) — не лише
// по перших 12 секундах. Це важливо: сегменти для збірки беруться з
// довільного місця кліпу (див. коментар "Ріжемо таймлайн на сегменти" на
// початку файлу), тож кліп із яскравим початком і темною серединою
// (типовий випадок — туман, що "накочує" за кілька секунд) інакше проходив
// би відсів, хоча в готовому відео давав би темну ділянку. Використовуємо,
// щоб відсіяти надто темні кліпи ще до нормалізації. Помилку зумисно не
// робимо фатальною: якщо виміряти не вдалось, кліп краще прийняти, ніж
// заблокувати запуск.
func meanLuma(path string, duration float64) (float64, error) {
	sampleDur := duration
	if sampleDur < minSourceDur {
		sampleDur = minSourceDur
	}
	fps := 12.0 / sampleDur
	cmd := exec.Command("ffmpeg", "-hide_banner", "-nostdin", "-loglevel", "info",
		"-i", path, "-vf", fmt.Sprintf("fps=%f,signalstats,metadata=print", fps),
		"-frames:v", "12", "-an", "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // код виходу не важливий — парсимо те, що встигло надрукуватись

	var sum float64
	var n int
	for _, line := range strings.Split(stderr.String(), "\n") {
		line = strings.TrimSpace(line)
		const key = "lavfi.signalstats.YAVG="
		if strings.HasPrefix(line, key) {
			if v, err := strconv.ParseFloat(strings.TrimPrefix(line, key), 64); err == nil {
				sum += v
				n++
			}
		}
	}
	if n == 0 {
		return 0, fmt.Errorf("meanLuma: не отримав YAVG для %s", filepath.Base(path))
	}
	return sum / float64(n), nil
}

// probeDuration повертає РЕАЛЬНУ тривалість файлу. Довіряти розрахункам не
// можна: setpts і кодек дають похибку, а вона накопичується й ламає offset
// у xfade — саме через це стара версія давала чорні вставки.
func probeDuration(path string) (float64, error) {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	d, err := strconv.ParseFloat(firstLine(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: не розібрав тривалість %q", filepath.Base(path), out)
	}
	return d, nil
}

// probeFrames рахує кадри реально, а не бере nb_frames з заголовка: у mpegts
// цього поля немає взагалі.
func probeFrames(path string) (int, error) {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0", "-count_packets",
		"-show_entries", "stream=nb_read_packets",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	// mpegts описує потік один раз на програму, тож ffprobe друкує значення
	// кілька разів — беремо перше.
	n, err := strconv.Atoi(firstLine(string(out)))
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: не розібрав кількість кадрів %q", filepath.Base(path), out)
	}
	return n, nil
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

func checkTools() error {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s не знайдено в PATH — встанови FFmpeg (winget install Gyan.FFmpeg)", bin)
		}
	}
	return nil
}

// ==================== КРОК 1: ЗАВАНТАЖЕННЯ НОВИХ КЛІПІВ ====================

type Source struct {
	ID       int
	Query    string
	Path     string
	Duration float64
}

var apiClient = &http.Client{Timeout: 30 * time.Second}
var dlClient = &http.Client{Timeout: 20 * time.Minute}

func searchPexels(query string, page int) ([]PexelsVideo, error) {
	q := url.Values{}
	q.Set("query", query)
	q.Set("per_page", "80")
	q.Set("page", strconv.Itoa(page))
	q.Set("orientation", "landscape")

	req, err := http.NewRequest("GET", "https://api.pexels.com/videos/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", cfg.APIKey)

	resp, err := apiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("запит до Pexels не вдався: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("Pexels: перевищено ліміт запитів (429), спробуй за годину")
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("Pexels повернув %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed PexelsSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("не вдалося розібрати відповідь Pexels: %w", err)
	}
	return parsed.Videos, nil
}

// freshCandidates відбирає з видачі тільки те, що ще не використовувалось,
// і сортує довші кліпи вперед — довгий вихідник = менш помітний луп.
func freshCandidates(videos []PexelsVideo, lib *Library) []PexelsVideo {
	var out []PexelsVideo
	for _, v := range videos {
		if lib.seen(v.ID) || float64(v.Duration) < minSourceDur {
			continue
		}
		if v.Height > 0 && v.Width < v.Height {
			continue // вертикальне відео нам не підходить
		}
		if pickBestFile(v) == nil {
			continue
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Duration > out[j].Duration })
	return out
}

// pickBestFile надає перевагу рендеру рівно 1920x1080: 4K качається довго
// й потім усе одно масштабується вниз.
func pickBestFile(v PexelsVideo) *PexelsVideoFile {
	var exact, bigger, fallback *PexelsVideoFile
	for i := range v.Files {
		f := &v.Files[i]
		if f.FileType != "" && !strings.Contains(f.FileType, "mp4") && !strings.HasSuffix(f.Link, ".mp4") {
			continue
		}
		if f.Width == 1920 && f.Height == 1080 {
			exact = f
		}
		if f.Width >= 1920 && (bigger == nil || f.Width < bigger.Width) {
			bigger = f
		}
		if fallback == nil || f.Width > fallback.Width {
			fallback = f
		}
	}
	switch {
	case exact != nil:
		return exact
	case bigger != nil:
		return bigger
	default:
		return fallback
	}
}

func downloadFile(link, outPath string) error {
	resp, err := dlClient.Get(link)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp := outPath + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, outPath)
}

// fetchNewClips бере cfg.Clips різних запитів і скачує з кожного по одному
// свіжому кліпу. Якщо запит нічого нового не дав — просто береться наступний
// із пулу, тому «вичерпані» теми не блокують запуск.
func fetchNewClips(lib *Library, rng *rand.Rand, want int) []Source {
	queries := append([]string{}, queryPool...)
	rng.Shuffle(len(queries), func(i, j int) { queries[i], queries[j] = queries[j], queries[i] })

	var got []Source
	for _, query := range queries {
		if len(got) >= want {
			break
		}

		// Випадкова сторінка видачі — щоб різні запуски черпали з різних
		// ділянок каталогу, а не довбали перші 15 результатів.
		var candidates []PexelsVideo
		for _, p := range rng.Perm(maxPagesToTry) {
			videos, err := searchPexels(query, p+1)
			if err != nil {
				fmt.Printf("   ⚠️  %q: %v\n", query, err)
				break
			}
			if len(videos) == 0 {
				continue
			}
			if candidates = freshCandidates(videos, lib); len(candidates) > 0 {
				break
			}
		}
		if len(candidates) == 0 {
			continue
		}

		// Не завжди найдовший: інакше однакові запити давали б однакові
		// кліпи. Тягнемо випадковий із трійки найдовших.
		top := candidates
		if len(top) > 3 {
			top = top[:3]
		}
		v := top[rng.Intn(len(top))]

		file := pickBestFile(v)
		raw := filepath.Join(workDir, fmt.Sprintf("raw_%d.mp4", v.ID))
		fmt.Printf("   ⬇️  %-32q ID %-9d %3ds  %dx%d\n", query, v.ID, v.Duration, file.Width, file.Height)

		if err := downloadFile(file.Link, raw); err != nil {
			fmt.Printf("      ⚠️  не скачалось: %v\n", err)
			continue
		}

		// Реєструємо ОДРАЗУ — навіть якщо далі щось піде не так (у т.ч. відсів
		// за темнотою), цей ID більше ніколи не повториться, тож той самий
		// темний кліп не качатиметься знову.
		lib.markUsed(UsedClip{ID: v.ID, Query: query, Duration: float64(v.Duration), UsedAt: time.Now(), Run: lib.Runs})

		// Відсів надто темних: пів-екрана чорного — не фон. Ніч лишається,
		// якщо в кадрі є світло (місто/сяйво/місяць дають YAVG значно вище).
		if lum, err := meanLuma(raw, float64(v.Duration)); err == nil && lum < minMeanLuma {
			fmt.Printf("      ⏭️  надто темний (YAVG %.0f < %.0f), пропускаю\n", lum, minMeanLuma)
			os.Remove(raw)
			continue
		}

		got = append(got, Source{ID: v.ID, Query: query, Path: raw, Duration: float64(v.Duration)})
	}
	return got
}

// ==================== КРОК 2: НОРМАЛІЗАЦІЯ + БЕЗШОВНИЙ ЛУП ====================

// prepareSource приводить вихідник до 1920x1080@30 один раз, щоб потім десятки
// шматків не декодували і не масштабували 4K заново. Довші за prepCapSec
// вихідники обрізаються: рендерити 11 хвилин заради 4 використаних — марно.
//
// Для коротких кліпів робимо безшовний луп: останні C секунд перетікають у
// перші C, тому місце склейки при повторі не читається.
// Схема: [C..D-C] + xfade([D-C..D], [0..C]) — кінець результату виглядає як
// його ж початок.
func prepareSource(src Source, enc Encoder) (Source, error) {
	dst := filepath.Join(workDir, fmt.Sprintf("prep_%d.mp4", src.ID))
	normalize := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=increase:flags=lanczos,crop=%d:%d,fps=%d,format=yuv420p,setsar=1",
		outWidth, outHeight, outWidth, outHeight, outFPS)

	d := math.Min(src.Duration, prepCapSec)
	c := math.Min(1.5, d*0.12)
	seamless := d >= 8 && c >= 0.5 && d-2*c >= 3

	build := func(withSeam bool) []string {
		var fc string
		if withSeam {
			fc = fmt.Sprintf(
				"[0:v]%s,split=3[h][b][t];"+
					"[h]trim=start=0:end=%.3f,setpts=PTS-STARTPTS[head];"+
					"[b]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS[body];"+
					"[t]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS[tail];"+
					"[tail][head]xfade=transition=fade:duration=%.3f:offset=0[blend];"+
					"[body][blend]concat=n=2:v=1[v]",
				normalize, c, c, d-c, d-c, d, c)
		} else {
			fc = fmt.Sprintf("[0:v]%s[v]", normalize)
		}
		args := []string{"-t", fmt.Sprintf("%.3f", d), "-i", src.Path,
			"-filter_complex", fc, "-map", "[v]", "-an", "-fps_mode", "cfr"}
		args = append(args, enc.encodeArgs()...)
		return append(args, dst)
	}

	err := runFFmpeg(build(seamless)...)
	if err != nil && seamless {
		// Безшовний луп — приємний бонус, але не привід валити запуск.
		err = runFFmpeg(build(false)...)
	}
	if err != nil {
		return src, err
	}

	realDur, err := probeDuration(dst)
	if err != nil {
		return src, err
	}
	return Source{ID: src.ID, Query: src.Query, Path: dst, Duration: realDur}, nil
}

// ==================== КРОК 3: ПЛАН СЕГМЕНТІВ ТА УНІКАЛІЗАЦІЯ ====================

// look — «характер» конкретного кліпу: стабільний зсув кольору й швидкості,
// щоб різні вихідники відрізнялися між собою, а не тільки сегмент від сегмента.
type look struct {
	hue   float64
	sat   float64
	warm  float64
	speed float64
	flip  bool
}

type Segment struct {
	Src    Source
	Start  float64 // з якої секунди вихідника починається сегмент
	Frames int     // довжина сегмента на виході, у кадрах

	Zoom         float64
	PanX0, PanX1 float64 // позиція кропу по X на початку / в кінці сегмента (лінійний дрейф)
	PanY0, PanY1 float64 // те саме по Y

	Hue      float64
	Sat      float64
	Bright   float64
	Contrast float64
	Gamma    float64
	Warm     float64
	Speed    float64
	Noise    int
	Sharpen  float64
	Flip     bool
}

func randRange(rng *rand.Rand, lo, hi float64) float64 { return lo + rng.Float64()*(hi-lo) }

// signOf повертає -1 або +1 з рівною ймовірністю — випадковий напрямок дрейфу.
func signOf(rng *rand.Rand) float64 {
	if rng.Float64() < 0.5 {
		return -1
	}
	return 1
}

func buildLooks(sources []Source, rng *rand.Rand) map[int]look {
	looks := make(map[int]look, len(sources))
	for _, s := range sources {
		looks[s.ID] = look{
			hue:   randRange(rng, -8, 8),
			sat:   randRange(rng, -0.06, 0.06),
			warm:  randRange(rng, -0.04, 0.04),
			speed: randRange(rng, -0.03, 0.03),
			flip:  rng.Float64() < 0.5,
		}
	}
	return looks
}

// buildPlan набирає сегменти, поки сумарна тривалість (з поправкою на те, що
// кожен кросфейд «з'їдає» crossfade секунд) не перекриє ціль.
//
// Сегмент ніколи не виходить за межі свого вихідника: це прибирає потребу в
// -stream_loop і дозволяє рендерити будь-який шматок таймлайну через миттєвий
// пошук по входу (-ss), а не декодуванням із самого початку.
func buildPlan(sources []Source, looks map[int]look, target float64, rng *rand.Rand) []Segment {
	// Запас у 1 секунду: перехід підглядає на пару кадрів далі за кінець
	// сегмента, і вийти за межі вихідника не можна.
	const tailMargin = 1.0

	minDur := math.Max(4*cfg.Crossfade, 8)
	var plan []Segment
	effective := 0.0
	bag := newSourceBag(sources, rng)

	for effective < target {
		src := bag.next()
		lk := looks[src.ID]
		speed := clamp(randRange(rng, 0.93, 1.07)+lk.speed, 0.88, 1.12)

		dur := randRange(rng, cfg.SegLen*0.75, cfg.SegLen*1.25)
		usable := src.Duration/speed - tailMargin
		if dur > usable {
			dur = usable
		}
		if dur < minDur {
			dur = math.Min(minDur, usable)
		}

		// Довжина сегмента — ціле число кадрів, інакше на шві з'явиться
		// зайвий кадр із дубльованим PTS.
		frames := int(math.Round(dur * outFPS))
		if frames < 2*cfFrames+outFPS {
			frames = 2*cfFrames + outFPS
		}
		dur = framesToSec(frames)

		// Випадковий вхід у кліп — головна причина, чому десяток повторів
		// одного вихідника не виглядає як десяток однакових шматків.
		start := 0.0
		if maxStart := src.Duration - dur*speed - tailMargin; maxStart > 0 {
			start = rng.Float64() * maxStart
		}

		// Помірний зум-кроп + ПОВІЛЬНИЙ МОНОТОННИЙ дрейф кадру наскрізь через
		// увесь сегмент (Ken Burns), без осциляції: картинка не хитається
		// туди-сюди, а плавно «пливе» в один бік — оку це приємно.
		zoom := randRange(rng, 1.05, 1.10)
		baseX := float64(evenInt(float64(outWidth)*zoom)-outWidth) / 2
		baseY := float64(evenInt(float64(outHeight)*zoom)-outHeight) / 2
		// Проходимо частину доступного діапазону в випадковий бік, через центр.
		spanX := baseX * randRange(rng, 0.5, 0.95) * signOf(rng)
		spanY := baseY * randRange(rng, 0.5, 0.95) * signOf(rng)
		panX0 := clamp(baseX-spanX/2, 0, 2*baseX)
		panX1 := clamp(baseX+spanX/2, 0, 2*baseX)
		panY0 := clamp(baseY-spanY/2, 0, 2*baseY)
		panY1 := clamp(baseY+spanY/2, 0, 2*baseY)

		plan = append(plan, Segment{
			Src:    src,
			Start:  start,
			Frames: frames,

			Zoom:  zoom,
			PanX0: panX0, PanX1: panX1,
			PanY0: panY0, PanY1: panY1,

			Hue:      clamp(randRange(rng, -10, 10)+lk.hue, -18, 18),
			Sat:      clamp(randRange(rng, 0.92, 1.10)+lk.sat, 0.85, 1.18),
			Bright:   randRange(rng, -0.03, 0.03),
			Contrast: randRange(rng, 0.95, 1.08),
			Gamma:    randRange(rng, 0.94, 1.07),
			Warm:     clamp(randRange(rng, -0.05, 0.05)+lk.warm, -0.08, 0.08),
			Speed:    speed,
			// Шум навмисно слабкий. Він добре ламає піксельні відбитки, але
			// при alls=7 відео стає майже нестисним: 29 Мбіт/с проти 6.5
			// на alls≤3 за тієї самої якості картинки.
			Noise:   1 + rng.Intn(3),
			Sharpen: randRange(rng, 0.15, 0.6),
			Flip:    lk.flip != (rng.Float64() < 0.25), // XOR: іноді інвертуємо «характер» кліпу
		})

		if len(plan) == 1 {
			effective += dur
		} else {
			effective += dur - cfg.Crossfade
		}
	}
	return plan
}

// planFrames — довжина всього таймлайну в кадрах. Кожен перехід «з'їдає»
// cfFrames, бо хвіст одного сегмента накладається на голову наступного.
func planFrames(plan []Segment) int {
	total := 0
	for _, s := range plan {
		total += s.Frames
	}
	return total - (len(plan)-1)*cfFrames
}

// fitPlan підганяє план рівно під ціль, підрізаючи сегменти з кінця.
//
// Без цього доводилося б обрізати готове відео через -t, а -t при -c copy
// ріже по межі пакета: у хвості лишалися діри й зайві кадри. Коли матеріал
// збігається з ціллю кадр у кадр, різати взагалі нічого не треба.
func fitPlan(plan []Segment, targetFrames int) []Segment {
	minFrames := 2*cfFrames + outFPS

	excess := planFrames(plan) - targetFrames
	for i := len(plan) - 1; i >= 0 && excess > 0; i-- {
		if room := plan[i].Frames - minFrames; room > 0 {
			cut := min(room, excess)
			plan[i].Frames -= cut
			excess -= cut
		}
	}
	for len(plan) > 2 && planFrames(plan)-plan[len(plan)-1].Frames+cfFrames >= targetFrames {
		plan = plan[:len(plan)-1] // цілий зайвий сегмент у хвості
	}
	return plan
}

// sourceBag видає вихідники перетасованими «колодами», щоб усі кліпи
// зустрічалися однаково часто й ніколи не йшли два однакових поспіль.
type sourceBag struct {
	all  []Source
	rng  *rand.Rand
	deck []Source
	last int
}

func newSourceBag(all []Source, rng *rand.Rand) *sourceBag {
	return &sourceBag{all: all, rng: rng, last: -1}
}

func (b *sourceBag) next() Source {
	if len(b.deck) == 0 {
		b.deck = append([]Source{}, b.all...)
		b.rng.Shuffle(len(b.deck), func(i, j int) { b.deck[i], b.deck[j] = b.deck[j], b.deck[i] })
		if len(b.deck) > 1 && b.deck[0].ID == b.last {
			b.deck[0], b.deck[len(b.deck)-1] = b.deck[len(b.deck)-1], b.deck[0]
		}
	}
	s := b.deck[0]
	b.deck = b.deck[1:]
	b.last = s.ID
	return s
}

// segFilter будує ланцюжок для ВІКНА сегмента, яке починається на fromT секунді
// його власного таймлайну. Вхід уже позиційований через -ss, тож час усередині
// фільтрів іде з нуля — саме тому в дрейфі кадру до t додається fromT: позиція
// кропу є функцією АБСОЛЮТНОГО часу сегмента, тому вона неперервна на межах
// шматків (body↔xfade↔body) і не смикається.
//
// setpts (швидкість) стоїть ПЕРШИМ, щоб t у виразах crop був часом ВИХОДУ.
func segFilter(seg Segment, fromT float64) string {
	scaledW := evenInt(float64(outWidth) * seg.Zoom)
	scaledH := evenInt(float64(outHeight) * seg.Zoom)
	segDur := framesToSec(seg.Frames)
	if segDur <= 0 {
		segDur = 1
	}

	f := []string{
		"setpts=PTS-STARTPTS",
		fmt.Sprintf("setpts=%.5f*PTS", 1/seg.Speed),
	}
	if seg.Flip {
		f = append(f, "hflip")
	}
	f = append(f,
		fmt.Sprintf("scale=%d:%d:flags=bicubic", scaledW, scaledH),

		// Повільний МОНОТОННИЙ дрейф кадру від PanX0 до PanX1 за тривалість
		// сегмента — лінійний за абсолютним часом (t+fromT), тому неперервний
		// через межі шматків і без хитання туди-сюди.
		fmt.Sprintf("crop=%d:%d:x=%.2f+(%.2f)*(t+%.3f)/%.3f:y=%.2f+(%.2f)*(t+%.3f)/%.3f",
			outWidth, outHeight,
			seg.PanX0, seg.PanX1-seg.PanX0, fromT, segDur,
			seg.PanY0, seg.PanY1-seg.PanY0, fromT, segDur),

		fmt.Sprintf("eq=brightness=%.3f:contrast=%.3f:saturation=%.3f:gamma=%.3f",
			seg.Bright, seg.Contrast, seg.Sat, seg.Gamma),
		fmt.Sprintf("hue=h=%.2f", seg.Hue),

		// Тонування «тепліше/холодніше» через зсув U/V. Раніше тут стояв
		// colorbalance — він працює лише в RGB, і конвертація yuv420p↔gbrp
		// на кожному кадрі різала швидкість утричі.
		fmt.Sprintf("lutyuv=u=clipval%+.0f:v=clipval%+.0f", -seg.Warm*60, seg.Warm*60),

		fmt.Sprintf("unsharp=5:5:%.2f:5:5:0", seg.Sharpen),
		fmt.Sprintf("noise=alls=%d:allf=t", seg.Noise),
		fmt.Sprintf("fps=%d", outFPS),
		"format=yuv420p",
		"setsar=1",
	)
	return strings.Join(f, ",")
}

// srcTime переводить час усередині сегмента в час усередині вихідного файлу.
func srcTime(seg Segment, t float64) float64 { return seg.Start + t*seg.Speed }

// renderBody рендерить шматок сегмента, що починається на кадрі fromFrame і
// триває рівно frames кадрів. Довжина задається через -frames:v, а не -t:
// тільки так шматки стикуються кадр у кадр.
func renderBody(seg Segment, fromFrame, frames int, out string, enc Encoder, ts bool) error {
	fromT := framesToSec(fromFrame)
	args := []string{
		"-ss", fmt.Sprintf("%.4f", srcTime(seg, fromT)),
		"-i", seg.Src.Path,
		"-vf", segFilter(seg, fromT),
		"-frames:v", strconv.Itoa(frames),
		"-an", "-fps_mode", "cfr",
	}
	args = append(args, enc.encodeArgs()...)
	if ts {
		args = append(args, "-f", "mpegts", "-muxdelay", "0", "-muxpreload", "0")
	}
	return runFFmpeg(append(args, out)...)
}

// renderTransition рендерить сам перехід: останні cfFrames кадрів сегмента a
// змішуються з першими cfFrames кадрами сегмента b.
//
// Обидві гілки беруть на два кадри більше, ніж потрібно: xfade з offset=0
// віддає перехід, а далі — хвіст другого входу, і -frames:v відрізає рівно
// потрібне. Запас страхує від того, що точний пошук по входу промахнеться
// на кадр.
func renderTransition(a, b Segment, tr string, out string, enc Encoder) error {
	c := framesToSec(cfFrames)
	branch := framesToSec(cfFrames + 2)
	fromA := framesToSec(a.Frames - cfFrames)

	fc := fmt.Sprintf(
		"[0:v]%s,trim=duration=%.4f,setpts=PTS-STARTPTS[a];"+
			"[1:v]%s,trim=duration=%.4f,setpts=PTS-STARTPTS[b];"+
			"[a][b]xfade=transition=%s:duration=%.4f:offset=0[v]",
		segFilter(a, fromA), branch, segFilter(b, 0), branch, tr, c)

	args := []string{
		"-ss", fmt.Sprintf("%.4f", srcTime(a, fromA)), "-i", a.Src.Path,
		"-ss", fmt.Sprintf("%.4f", srcTime(b, 0)), "-i", b.Src.Path,
		"-filter_complex", fc, "-map", "[v]",
		"-frames:v", strconv.Itoa(cfFrames),
		"-an", "-fps_mode", "cfr",
	}
	args = append(args, enc.encodeArgs()...)
	args = append(args, "-f", "mpegts", "-muxdelay", "0", "-muxpreload", "0")
	return runFFmpeg(append(args, out)...)
}

// ==================== КРОК 4: СКЛЕЙКА ====================

// buildPieces розкладає таймлайн на незалежні шматки:
//
//	body_0, xfade_0, body_1, xfade_1, ..., body_n-1
//
// Кожен рендериться рівно один раз і рівно з тими параметрами кодування, що й
// решта, тому фінал збирається демуксером БЕЗ повторного кодування. Класичний
// ланцюжок xfade замість цього перекодовував усе відео ще двічі.
type piece struct {
	out    string
	frames int
	render func(Encoder) error
}

func buildPieces(plan []Segment, rng *rand.Rand) []piece {
	var pieces []piece

	for i := range plan {
		seg := plan[i]
		from, frames := 0, seg.Frames
		if i > 0 { // початок з'їдає попередній перехід
			from, frames = cfFrames, frames-cfFrames
		}
		if i < len(plan)-1 { // кінець з'їсть наступний
			frames -= cfFrames
		}

		out := filepath.Join(workDir, fmt.Sprintf("p%04d_body.ts", i))
		f, n := from, frames
		pieces = append(pieces, piece{out, n, func(e Encoder) error {
			return renderBody(seg, f, n, out, e, true)
		}})

		if i < len(plan)-1 {
			a, b := plan[i], plan[i+1]
			tr := transitions[rng.Intn(len(transitions))]
			xout := filepath.Join(workDir, fmt.Sprintf("p%04d_xf.ts", i))
			pieces = append(pieces, piece{xout, cfFrames, func(e Encoder) error {
				return renderTransition(a, b, tr, xout, e)
			}})
		}
	}
	return pieces
}

// concatPieces збирає фінал: склейка без перекодування, точна тривалість і
// тиха аудіодоріжка (без неї частина плеєрів і завантажувачів поводиться дивно).
func concatPieces(paths []string, out string, target float64) error {
	listPath := filepath.Join(workDir, "concat.txt")
	var sb strings.Builder
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(&sb, "file '%s'\n", strings.ReplaceAll(abs, "\\", "/"))
	}
	if err := os.WriteFile(listPath, []byte(sb.String()), 0644); err != nil {
		return err
	}

	// Відео НЕ обрізаємо: план уже підігнано під ціль кадр у кадр, а -t при
	// -c copy ріже по межі пакета й лишає в хвості діри. Довжину задає відео,
	// тиша просто береться із запасом і підрізається через -shortest.
	return runFFmpeg(
		"-fflags", "+genpts",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-f", "lavfi", "-t", fmt.Sprintf("%.3f", target+2), "-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
		"-map", "0:v:0", "-map", "1:a:0",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "128k", "-shortest",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart", out)
}

// --- запасний режим -transition merge: класичний ланцюжок xfade ---

type rendered struct {
	Path string
	Dur  float64
}

// mergeXfade зшиває кілька файлів одним ланцюжком xfade. Offset кожного
// переходу рахується від НАКОПИЧЕНОЇ реальної тривалості — це і є те, що
// стара версія робила неправильно.
func mergeXfade(parts []rendered, out string, enc Encoder, rng *rand.Rand) error {
	if len(parts) == 1 {
		return runFFmpeg("-i", parts[0].Path, "-c", "copy", out)
	}

	var args []string
	for _, p := range parts {
		args = append(args, "-i", p.Path)
	}

	var fc strings.Builder
	acc := parts[0].Dur
	label := "[0:v]"
	for i := 1; i < len(parts); i++ {
		offset := math.Max(acc-cfg.Crossfade, 0.1)
		next := fmt.Sprintf("[x%d]", i)
		fmt.Fprintf(&fc, "%s[%d:v]xfade=transition=%s:duration=%.3f:offset=%.3f%s;",
			label, i, transitions[rng.Intn(len(transitions))], cfg.Crossfade, offset, next)
		acc += parts[i].Dur - cfg.Crossfade
		label = next
	}

	args = append(args, "-filter_complex", strings.TrimSuffix(fc.String(), ";"),
		"-map", label, "-an", "-fps_mode", "cfr")
	args = append(args, enc.encodeArgs()...)
	return runFFmpeg(append(args, out)...)
}

// mergeAll зводить сегменти в один файл партіями по cfg.MergeBatch: один
// виклик ffmpeg на 40+ входів відкрив би 40 декодерів одночасно.
func mergeAll(parts []rendered, enc Encoder, rng *rand.Rand) (string, error) {
	for level := 1; len(parts) > 1; level++ {
		var next []rendered
		var consumed []string

		fmt.Printf("   рівень %d: %d файлів → %d\n", level, len(parts),
			(len(parts)+cfg.MergeBatch-1)/cfg.MergeBatch)

		for i := 0; i < len(parts); i += cfg.MergeBatch {
			j := min(i+cfg.MergeBatch, len(parts))
			if j-i == 1 {
				next = append(next, parts[i]) // непарний хвіст переносимо як є
				continue
			}

			out := filepath.Join(workDir, fmt.Sprintf("merge_l%d_%02d.mp4", level, i/cfg.MergeBatch))
			if err := mergeXfade(parts[i:j], out, enc, rng); err != nil {
				return "", err
			}
			d, err := probeDuration(out)
			if err != nil {
				return "", err
			}
			for _, p := range parts[i:j] {
				consumed = append(consumed, p.Path)
			}
			next = append(next, rendered{Path: out, Dur: d})
		}

		if !cfg.Keep {
			for _, p := range consumed {
				os.Remove(p)
			}
		}
		parts = next
	}
	return parts[0].Path, nil
}

// finalize обрізає до точної тривалості та додає тиху доріжку. Відео копіюється.
func finalize(src, out string, target float64) error {
	return runFFmpeg(
		"-i", src,
		"-f", "lavfi", "-t", fmt.Sprintf("%.3f", target), "-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
		"-map", "0:v:0", "-map", "1:a:0",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "128k",
		"-t", fmt.Sprintf("%.3f", target),
		"-movflags", "+faststart", out)
}

// ==================== ДОПОМІЖНЕ ====================

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func evenInt(v float64) int {
	n := int(math.Round(v))
	if n%2 != 0 {
		n++
	}
	return n
}

func fmtDur(sec float64) string {
	d := time.Duration(sec * float64(time.Second))
	return fmt.Sprintf("%02d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

// runJobs виконує завдання пулом воркерів, зберігаючи порядок результатів.
// Кожне впале завдання отримує одну повторну спробу: збій одного шматка
// розриває таймлайн, тому мовчки пропускати його не можна.
func runJobs(n int, started time.Time, work func(i int) error) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
		errs []error
		jobs = make(chan int)
	)

	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				err := work(i)
				if err != nil {
					err = work(i) // одна повторна спроба
				}
				mu.Lock()
				done++
				if err != nil {
					errs = append(errs, fmt.Errorf("шматок %d: %w", i, err))
				} else if done%5 == 0 || done == n {
					elapsed := time.Since(started)
					eta := time.Duration(float64(elapsed) / float64(done) * float64(n-done))
					fmt.Printf("   [%4d/%4d] залишилось ~%s\n", done, n, eta.Round(time.Second))
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("не вдалося відрендерити %d шматків, перший: %w", len(errs), errs[0])
	}
	return nil
}

// ==================== ГОЛОВНА ЛОГІКА ====================

func main() {
	flag.Parse()
	started := time.Now()

	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("PEXELS_API_KEY")
	}
	if cfg.APIKey == "" {
		cfg.APIKey = defaultAPIKey
	}
	if cfg.Out == "" {
		cfg.Out = fmt.Sprintf("background_%s.mp4", started.Format("2006-01-02_15-04-05"))
	}
	if cfg.Workers <= 0 {
		cfg.Workers = min(max(runtime.NumCPU()/2, 2), 6)
	}
	if cfg.Clips < 2 {
		cfg.Clips = 2
	}
	if cfg.Seed == 0 {
		cfg.Seed = started.UnixNano()
	}
	rng := rand.New(rand.NewSource(cfg.Seed))
	target := cfg.Minutes * 60

	cfFrames = max(int(math.Round(cfg.Crossfade*outFPS)), 2)
	cfg.Crossfade = framesToSec(cfFrames)

	if err := checkTools(); err != nil {
		fmt.Println("❌", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		fmt.Println("❌ Не вдалося створити робочу папку:", err)
		os.Exit(1)
	}

	lib := loadLibrary()
	lib.Runs++
	fmt.Printf("🎬 Запуск #%d · ціль %s · %d нових кліпів · seed %d\n", lib.Runs, fmtDur(target), cfg.Clips, cfg.Seed)
	fmt.Printf("📋 У реєстрі вже %d використаних кліпів — жоден із них не повториться\n\n", len(lib.Used))

	enc := pickEncoder()
	fmt.Printf("⚙️  Кодек: %s · паралельно: %d · переходи: %s\n", enc.Name, cfg.Workers, cfg.Transition)
	if enc.IsCPU {
		fmt.Println("   ℹ️  Апаратний кодер недоступний. З робочим NVENC/QSV рендер у рази швидший.")
	}
	fmt.Println()

	// --- Крок 1: качаємо ТІЛЬКИ нові кліпи ---
	fmt.Println("⬇️  Шукаю нові кліпи на Pexels...")
	raw := fetchNewClips(lib, rng, cfg.Clips)
	if len(raw) < 2 {
		fmt.Printf("\n❌ Вдалося дістати лише %d кліпів. Перевір інтернет/ключ; якщо реєстр величезний — розшир queryPool.\n", len(raw))
		os.Exit(1)
	}
	if len(raw) < cfg.Clips {
		fmt.Printf("   ⚠️  Знайшлося %d із %d кліпів — продовжую з тим, що є\n", len(raw), cfg.Clips)
	}

	// --- Крок 2: нормалізація до 1920x1080@30 + безшовний луп ---
	fmt.Println("\n🔧 Готую вихідники (1920x1080@30, безшовний луп)...")
	prepped := make([]*Source, len(raw))
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, cfg.Workers)
	for i, s := range raw {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			prep, err := prepareSource(s, enc)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fmt.Printf("   ⚠️  ID %d пропускаю: %v\n", s.ID, err)
				return
			}
			fmt.Printf("   ✔️  ID %-9d %s\n", prep.ID, fmtDur(prep.Duration))
			prepped[i] = &prep
		}()
	}
	wg.Wait()

	var sources []Source
	for i, p := range prepped {
		if p != nil {
			sources = append(sources, *p)
		}
		if !cfg.Keep {
			os.Remove(raw[i].Path) // сирий вихідник більше не потрібен
		}
	}
	if len(sources) < 2 {
		fmt.Println("\n❌ Після обробки лишилося менше 2 придатних кліпів.")
		os.Exit(1)
	}

	// --- Крок 3: план сегментів ---
	looks := buildLooks(sources, rng)
	plan := fitPlan(buildPlan(sources, looks, target, rng), int(math.Round(target*outFPS)))

	if cfg.Transition == "merge" {
		runMergeMode(plan, enc, rng, started, target)
	} else {
		runFastMode(plan, enc, rng, started, target)
	}

	// --- Прибирання ---
	if !cfg.Keep {
		entries, _ := os.ReadDir(workDir)
		for _, e := range entries {
			os.Remove(filepath.Join(workDir, e.Name()))
		}
	}
	if err := lib.save(); err != nil {
		fmt.Printf("⚠️  Не вдалося зберегти %s: %v\n", libraryFile, err)
	}

	// --- Підсумок ---
	realDur, _ := probeDuration(cfg.Out)
	sizeMB := 0.0
	if info, err := os.Stat(cfg.Out); err == nil {
		sizeMB = float64(info.Size()) / 1024 / 1024
	}

	fmt.Printf("\n✅ Готово за %s\n", time.Since(started).Round(time.Second))
	fmt.Printf("   Файл:       %s\n", cfg.Out)
	fmt.Printf("   Тривалість: %s (%.0f МБ, 1920x1080@%d)\n", fmtDur(realDur), sizeMB, outFPS)
	fmt.Println("   Кліпи цього запуску:")
	for _, s := range sources {
		fmt.Printf("     · %-32q https://www.pexels.com/video/%d/\n", s.Query, s.ID)
	}
	fmt.Printf("   Реєстр: %d використаних кліпів — наступний запуск візьме інші.\n", len(lib.Used))
}

// runFastMode — основний шлях: рендеримо кожен шматок таймлайну рівно один
// раз і склеюємо демуксером без перекодування.
func runFastMode(plan []Segment, enc Encoder, rng *rand.Rand, started time.Time, target float64) {
	pieces := buildPieces(plan, rng)
	fmt.Printf("\n🎨 Рендерю %d сегментів (%d шматків), кожен зі своєю унікалізацією...\n", len(plan), len(pieces))

	if err := runJobs(len(pieces), started, func(i int) error { return pieces[i].render(enc) }); err != nil {
		fmt.Println("\n❌", err)
		os.Exit(1)
	}

	// Склейка без перекодування вимагає, щоб кожен шматок мав РІВНО стільки
	// кадрів, скільки планувалося: зайвий кадр зсуває весь наступний таймлайн
	// і дає дубльований PTS на шві.
	paths := make([]string, len(pieces))
	totalFrames := 0
	for i, p := range pieces {
		paths[i] = p.out
		n, err := probeFrames(p.out)
		if err != nil {
			fmt.Println("\n❌", err)
			os.Exit(1)
		}
		if n != p.frames {
			fmt.Printf("   ⚠️  %s: %d кадрів замість %d\n", filepath.Base(p.out), n, p.frames)
		}
		totalFrames += n
	}

	if total := framesToSec(totalFrames); math.Abs(total-target) > 0.5 {
		fmt.Printf("   ⚠️  Матеріалу на %s замість %s\n", fmtDur(total), fmtDur(target))
		target = total
	}

	fmt.Printf("\n🔗 Склеюю %d шматків без перекодування...\n", len(paths))
	if err := concatPieces(paths, cfg.Out, target); err != nil {
		fmt.Println("❌ Помилка склейки:", err)
		os.Exit(1)
	}
}

// runMergeMode — запасний шлях: повні сегменти + класичний ланцюжок xfade.
// Втричі повільніше, зате не залежить від склейки без перекодування.
func runMergeMode(plan []Segment, enc Encoder, rng *rand.Rand, started time.Time, target float64) {
	fmt.Printf("\n🎨 Рендерю %d сегментів, кожен зі своєю унікалізацією...\n", len(plan))

	parts := make([]rendered, len(plan))
	err := runJobs(len(plan), started, func(i int) error {
		out := filepath.Join(workDir, fmt.Sprintf("seg_%04d.mp4", i))
		if err := renderBody(plan[i], 0, plan[i].Frames, out, enc, false); err != nil {
			return err
		}
		d, err := probeDuration(out)
		if err != nil {
			return err
		}
		parts[i] = rendered{Path: out, Dur: d}
		return nil
	})
	if err != nil {
		fmt.Println("\n❌", err)
		os.Exit(1)
	}

	total := parts[0].Dur
	for _, p := range parts[1:] {
		total += p.Dur - cfg.Crossfade
	}
	if total < target {
		fmt.Printf("   ⚠️  Матеріалу на %s замість %s — фінал буде коротшим\n", fmtDur(total), fmtDur(target))
		target = total - 0.5
	}

	fmt.Printf("\n🔗 Склеюю %d сегментів...\n", len(parts))
	merged, err := mergeAll(parts, enc, rng)
	if err != nil {
		fmt.Println("❌ Помилка склейки:", err)
		os.Exit(1)
	}

	fmt.Println("\n📦 Фіналізую (точна тривалість, тиха доріжка, faststart)...")
	if err := finalize(merged, cfg.Out, target); err != nil {
		fmt.Println("❌ Помилка фіналізації:", err)
		os.Exit(1)
	}
}
