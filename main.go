package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

//go:embed 123.png
var iconPNG []byte

func loadAppIcon() *walk.Icon {
	if img, _, err := image.Decode(bytes.NewReader(iconPNG)); err == nil {
		if icon, err := walk.NewIconFromImageForDPI(img, 96); err == nil {
			return icon
		}
	}
	icon, _ := walk.NewIconFromSysDLL("shell32", 24)
	return icon
}

// ---------- config ----------

type Config struct {
	IntervalMinutes int    `json:"interval_minutes"`
	UseTimeWindow   bool   `json:"use_time_window"`
	StartTime       string `json:"start_time"` // HH:MM
	EndTime         string `json:"end_time"`   // HH:MM
	AutoStart       bool   `json:"auto_start"`
}

var defaultConfig = Config{
	IntervalMinutes: 4,
	UseTimeWindow:   false,
	StartTime:       "09:00",
	EndTime:         "17:30",
	AutoStart:       false,
}

var intervalOptions = []int{1, 2, 4, 5, 10, 15, 30}

func configPath() string {
	exe, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		return filepath.Join(filepath.Dir(exe), ".unlock.json")
	}
	// 兜底：当前工作目录
	return ".unlock.json"
}

func loadConfig() Config {
	cfg := defaultConfig
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = saveConfig(cfg) // 首次启动落盘默认配置
		}
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.IntervalMinutes <= 0 {
		cfg.IntervalMinutes = defaultConfig.IntervalMinutes
	}
	if cfg.StartTime == "" {
		cfg.StartTime = defaultConfig.StartTime
	}
	if cfg.EndTime == "" {
		cfg.EndTime = defaultConfig.EndTime
	}
	return cfg
}

func saveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func indexOfInterval(m int) int {
	for i, v := range intervalOptions {
		if v == m {
			return i
		}
	}
	return 2 // 默认 4 分钟
}

// ---------- time window ----------

func parseHM(s string) (h, m int, ok bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hh, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	mm, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

func inWindow(now time.Time, start, end string) bool {
	sh, sm, ok1 := parseHM(start)
	eh, em, ok2 := parseHM(end)
	if !ok1 || !ok2 {
		return true // 配错就视为始终生效，避免静默不工作
	}
	cur := now.Hour()*60 + now.Minute()
	s := sh*60 + sm
	e := eh*60 + em
	if s == e {
		return true
	}
	if s < e {
		return cur >= s && cur < e
	}
	// 跨午夜，比如 22:00-06:00
	return cur >= s || cur < e
}

// ---------- worker ----------

var (
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
)

func startWorker(cfg Config) {
	mu.Lock()
	defer mu.Unlock()
	if running {
		return
	}
	running = true
	stopCh = make(chan struct{})
	go worker(cfg, stopCh)
}

func stopWorker() {
	mu.Lock()
	defer mu.Unlock()
	if !running {
		return
	}
	running = false
	close(stopCh)
}

func worker(cfg Config, stop <-chan struct{}) {
	interval := time.Duration(cfg.IntervalMinutes) * time.Minute
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			if cfg.UseTimeWindow && !inWindow(now, cfg.StartTime, cfg.EndTime) {
				continue
			}
			sendF15()
		}
	}
}

// ---------- UI ----------

func main() {
	cfg := loadConfig()

	var mw *walk.MainWindow
	var combo *walk.ComboBox
	var winCheck, autoCheck *walk.CheckBox
	var startHour, startMin, endHour, endMin *walk.ComboBox
	var startBtn, stopBtn *walk.PushButton
	var status *walk.Label

	intervalItems := make([]string, 0, len(intervalOptions))
	for _, m := range intervalOptions {
		intervalItems = append(intervalItems, fmt.Sprintf("%d 分钟", m))
	}

	hourItems := make([]string, 24)
	for i := 0; i < 24; i++ {
		hourItems[i] = fmt.Sprintf("%02d", i)
	}
	minItems := make([]string, 0, 12)
	for m := 0; m < 60; m += 5 {
		minItems = append(minItems, fmt.Sprintf("%02d", m))
	}
	hourIndex := func(hm string, def int) int {
		if h, _, ok := parseHM(hm); ok {
			return h
		}
		return def
	}
	minIndex := func(hm string, def int) int {
		if _, m, ok := parseHM(hm); ok {
			return m / 5 // 向下舍入到 5 分钟
		}
		return def
	}

	syncWindowEditsEnabled := func() {
		on := winCheck.Checked()
		startHour.SetEnabled(on)
		startMin.SetEnabled(on)
		endHour.SetEnabled(on)
		endMin.SetEnabled(on)
	}

	collectConfig := func() (Config, error) {
		idx := combo.CurrentIndex()
		if idx < 0 {
			idx = indexOfInterval(defaultConfig.IntervalMinutes)
		}
		c := Config{
			IntervalMinutes: intervalOptions[idx],
			UseTimeWindow:   winCheck.Checked(),
			StartTime: fmt.Sprintf("%s:%s",
				hourItems[startHour.CurrentIndex()],
				minItems[startMin.CurrentIndex()]),
			EndTime: fmt.Sprintf("%s:%s",
				hourItems[endHour.CurrentIndex()],
				minItems[endMin.CurrentIndex()]),
			AutoStart: autoCheck.Checked(),
		}
		return c, nil
	}

	statusForRunning := func(c Config) string {
		s := fmt.Sprintf("运行中 · 每 %d 分钟", c.IntervalMinutes)
		if c.UseTimeWindow {
			now := time.Now()
			in := inWindow(now, c.StartTime, c.EndTime)
			tag := "等待时间窗口"
			if in {
				tag = "时间窗口内"
			}
			s += fmt.Sprintf(" · %s（%s-%s）", tag, c.StartTime, c.EndTime)
		}
		return s
	}

	doStart := func(c Config) {
		startWorker(c)
		startBtn.SetEnabled(false)
		stopBtn.SetEnabled(true)
		combo.SetEnabled(false)
		winCheck.SetEnabled(false)
		startHour.SetEnabled(false)
		startMin.SetEnabled(false)
		endHour.SetEnabled(false)
		endMin.SetEnabled(false)
		status.SetText(statusForRunning(c))
	}

	doStop := func() {
		stopWorker()
		startBtn.SetEnabled(true)
		stopBtn.SetEnabled(false)
		combo.SetEnabled(true)
		winCheck.SetEnabled(true)
		syncWindowEditsEnabled()
		status.SetText("已停止")
	}

	startVisible := !cfg.AutoStart // 自启时直接进托盘
	appIcon := loadAppIcon()

	if err := (MainWindow{
		AssignTo: &mw,
		Title:    "unlock",
		Icon:     appIcon,
		Visible:  startVisible,
		MinSize:  Size{Width: 460, Height: 280},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: Grid{Columns: 2, MarginsZero: false},
				Children: []Widget{
					Label{Text: "间隔："},
					ComboBox{
						AssignTo:     &combo,
						Model:        intervalItems,
						CurrentIndex: indexOfInterval(cfg.IntervalMinutes),
					},
					CheckBox{
						AssignTo:   &winCheck,
						Text:       "仅在时间窗口内生效",
						ColumnSpan: 2,
						Checked:    cfg.UseTimeWindow,
						OnCheckedChanged: func() {
							syncWindowEditsEnabled()
						},
					},
					Composite{
						ColumnSpan: 2,
						Layout:     HBox{MarginsZero: true},
						Children: []Widget{
							Label{Text: "开始 "},
							ComboBox{
								AssignTo:     &startHour,
								Model:        hourItems,
								CurrentIndex: hourIndex(cfg.StartTime, 9),
								MaxSize:      Size{Width: 60},
							},
							Label{Text: ":"},
							ComboBox{
								AssignTo:     &startMin,
								Model:        minItems,
								CurrentIndex: minIndex(cfg.StartTime, 0),
								MaxSize:      Size{Width: 60},
							},
							Label{Text: "   结束 "},
							ComboBox{
								AssignTo:     &endHour,
								Model:        hourItems,
								CurrentIndex: hourIndex(cfg.EndTime, 17),
								MaxSize:      Size{Width: 60},
							},
							Label{Text: ":"},
							ComboBox{
								AssignTo:     &endMin,
								Model:        minItems,
								CurrentIndex: minIndex(cfg.EndTime, 6), // 6*5=30
								MaxSize:      Size{Width: 60},
							},
							HSpacer{},
						},
					},
					CheckBox{
						AssignTo:   &autoCheck,
						Text:       "启动时自动开始（直接进托盘）",
						ColumnSpan: 2,
						Checked:    cfg.AutoStart,
					},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					PushButton{
						AssignTo: &startBtn,
						Text:     "开始",
						OnClicked: func() {
							c, err := collectConfig()
							if err != nil {
								walk.MsgBox(mw, "配置无效", err.Error(), walk.MsgBoxIconWarning)
								return
							}
							_ = saveConfig(c)
							doStart(c)
						},
					},
					PushButton{
						AssignTo: &stopBtn,
						Text:     "停止",
						Enabled:  false,
						OnClicked: func() {
							doStop()
						},
					},
					PushButton{
						Text: "保存配置",
						OnClicked: func() {
							c, err := collectConfig()
							if err != nil {
								walk.MsgBox(mw, "配置无效", err.Error(), walk.MsgBoxIconWarning)
								return
							}
							if err := saveConfig(c); err != nil {
								walk.MsgBox(mw, "保存失败", err.Error(), walk.MsgBoxIconError)
								return
							}
							status.SetText("配置已保存到 " + configPath())
						},
					},
				},
			},
			Label{
				AssignTo: &status,
				Text:     "未运行 · 关闭窗口将最小化到托盘",
			},
			VSpacer{},
		},
	}.Create()); err != nil {
		panic(err)
	}

	syncWindowEditsEnabled()

	icon := appIcon
	if ni, err := walk.NewNotifyIcon(mw); err == nil {
		defer ni.Dispose()
		if icon != nil {
			_ = ni.SetIcon(icon)
		}
		_ = ni.SetToolTip("unlock")
		_ = ni.SetVisible(true)

		showAction := walk.NewAction()
		_ = showAction.SetText("显示窗口")
		_ = showAction.Triggered().Attach(func() {
			mw.Show()
			winShowWindow(uintptr(mw.Handle()), 9) // SW_RESTORE
			mw.SetFocus()
		})
		_ = ni.ContextMenu().Actions().Add(showAction)

		exitAction := walk.NewAction()
		_ = exitAction.SetText("退出")
		_ = exitAction.Triggered().Attach(func() {
			stopWorker()
			walk.App().Exit(0)
		})
		_ = ni.ContextMenu().Actions().Add(exitAction)

		ni.MouseDown().Attach(func(x, y int, btn walk.MouseButton) {
			if btn == walk.LeftButton {
				mw.Show()
				winShowWindow(uintptr(mw.Handle()), 9)
				mw.SetFocus()
			}
		})
	}

	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = true
		mw.Hide()
	})

	// 周期刷新状态行（让"时间窗口内/等待"实时更新）
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			mu.Lock()
			isRunning := running
			mu.Unlock()
			if !isRunning {
				continue
			}
			mw.Synchronize(func() {
				c, err := collectConfig()
				if err != nil {
					return
				}
				status.SetText(statusForRunning(c))
			})
		}
	}()

	if cfg.AutoStart {
		// 自启：用配置直接跑，窗口已经因 Visible:false 隐藏
		if _, _, ok1 := parseHM(cfg.StartTime); cfg.UseTimeWindow && !ok1 {
			cfg.UseTimeWindow = false // 配置坏掉就回退
		}
		doStart(cfg)
	}

	mw.Run()
}

// ---------- Win32 ----------

var (
	user32        = syscall.NewLazyDLL("user32.dll")
	procSendInput = user32.NewProc("SendInput")
	procShowWin   = user32.NewProc("ShowWindow")
)

const (
	inputKeyboard  = 1
	keyEventfKeyUp = 0x0002
	vkF15          = 0x7E
)

type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// INPUT 在 amd64 上总大小 40 字节（union 部分按 MOUSEINPUT 对齐）。
type input struct {
	inputType uint32
	_         [4]byte
	ki        keybdInput
	_         [8]byte
}

func sendF15() {
	inputs := [2]input{
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkF15}},
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkF15, dwFlags: keyEventfKeyUp}},
	}
	procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
}

func winShowWindow(hwnd uintptr, cmd int32) {
	procShowWin.Call(hwnd, uintptr(cmd))
}
