package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

type Color struct{ r, g, b int }

func rgb(c Color, s string) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", c.r, c.g, c.b, s)
}

type CalendarResponse struct {
	Status bool                 `json:"status"`
	Result map[string]MonthData `json:"result"`
}

type MonthData map[string]DayData

type DayData struct {
	Solar   DateInfo `json:"solar"`
	Holiday bool     `json:"holiday"`
	Event   []string `json:"event"`
}

type DateInfo struct {
	Day     int    `json:"day"`
	Month   int    `json:"month"`
	Year    int    `json:"year"`
	DayWeek string `json:"dayWeek"`
}

func fetchHolidays(year int) (map[string]string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache directory: %v", err)
	}

	cacheFile := filepath.Join(cacheDir, "shamsy_calendar", fmt.Sprintf("holidays_%d.json", year))

	if cachedHolidays, err := readFromCache(cacheFile); err == nil {
		return cachedHolidays, nil
	}

	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetDescription("Fetching holidays... \n"),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionSetWidth(20),
	)

	defer bar.Close()

	url := fmt.Sprintf("https://pnldev.com/api/calender?year=%d&holiday=true", year)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch holidays: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	var calendar CalendarResponse
	if err := json.Unmarshal(body, &calendar); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	if !calendar.Status {
		return nil, fmt.Errorf("API returned status false")
	}

	holidays := make(map[string]string)
	for _, days := range calendar.Result {
		for _, dayData := range days {
			if dayData.Holiday {
				key := fmt.Sprintf("%d-%02d-%02d", dayData.Solar.Year, dayData.Solar.Month, dayData.Solar.Day)
				if len(dayData.Event) > 0 {
					holidays[key] = strings.Join(dayData.Event, "; ")
				} else {
					holidays[key] = "Holiday"
				}
			}
		}
	}

	if err := saveToCache(cacheFile, holidays); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save to cache: %v\n", err)
	}

	return holidays, nil
}

func readFromCache(cacheFile string) (map[string]string, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, err
	}

	var holidays map[string]string
	if err := json.Unmarshal(data, &holidays); err != nil {
		return nil, err
	}

	return holidays, nil
}

func saveToCache(cacheFile string, holidays map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %v", err)
	}

	data, err := json.Marshal(holidays)
	if err != nil {
		return fmt.Errorf("failed to marshal holidays to JSON: %v", err)
	}

	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %v", err)
	}

	return nil
}

var (
	offday = Color{255, 0, 0}
	red    = Color{255, 255, 255}
	green  = Color{188, 188, 188}
	blue   = Color{135, 206, 235}
	yellow = Color{255, 255, 0}
)

var shamsyMonths = []string{
	"Farvardin", "Ordibehesht", "Khordad", "Tir", "Mordad", "Shahrivar",
	"Mehr", "Aban", "Azar", "Dey", "Bahman", "Esfand",
}

var weekDays = []string{"Sh", "Ye", "Do", "Se", "Ch", "Pa", "Jo"}

var goToshamsyWeekday = []int{1, 2, 3, 4, 5, 6, 0}

func isshamsyLeapYear(year int) bool {
	leapYears := []int{1, 5, 9, 13, 17, 22, 26, 30}
	cycle := (year - 474) % 2820
	mod := cycle % 33
	for _, v := range leapYears {
		if mod == v {
			return true
		}
	}
	return false
}

func shamsyMonthDays(year, month int) int {
	if month <= 6 {
		return 31
	} else if month <= 11 {
		return 30
	} else if month == 12 {
		if isshamsyLeapYear(year) {
			return 30
		}
		return 29
	}
	return 0
}

func gregorianToshamsy(gy, gm, gd int) (int, int, int) {
	g_d_m := [...]int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	var jy int
	if gy > 1600 {
		jy, gy = 979, gy-1600
	} else {
		jy, gy = 0, gy-621
	}
	gy2 := gy
	if gm <= 2 {
		gy2 = gy - 1
	}
	days := 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 - 80 + gd + g_d_m[gm-1]
	jy += 33 * (days / 12053)
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	jm := 1 + days/31
	jd := 1 + days%31
	if days >= 186 {
		jm = 7 + (days-186)/30
		jd = 1 + (days-186)%30
	}
	return jy, jm, jd
}

func shamsyToGregorian(jy, jm, jd int) (int, int, int) {
	gy := jy + 621
	days := -355668 + 365*jy + (jy/33)*8 + ((jy%33 + 3) / 4) + jd - 1
	for i := 1; i < jm; i++ {
		if i <= 6 {
			days += 31
		} else {
			days += 30
		}
	}
	g_day_no := days + 79
	gy = 1600 + 400*(g_day_no/146097)
	g_day_no = g_day_no % 146097
	leap := true
	if g_day_no >= 36525 {
		g_day_no--
		gy += 100 * (g_day_no / 36524)
		g_day_no = g_day_no % 36524
		if g_day_no >= 365 {
			g_day_no++
		} else {
			leap = false
		}
	}
	gy += 4 * (g_day_no / 1461)
	g_day_no %= 1461
	if g_day_no >= 366 {
		leap = false
		g_day_no--
		gy += g_day_no / 365
		g_day_no %= 365
	}
	gm := 0
	days_in_month := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if leap {
		days_in_month[1] = 29
	}
	for g_day_no >= days_in_month[gm] && gm < 12 {
		g_day_no -= days_in_month[gm]
		gm++
	}
	gm++
	gd := g_day_no + 1
	return gy, gm, gd
}

func getFirstWeekday(jy, jm int) int {
	gy, gm, gd := shamsyToGregorian(jy, jm, 1)
	t := time.Date(gy, time.Month(gm), gd, 0, 0, 0, 0, time.UTC)
	return goToshamsyWeekday[int(t.Weekday())]
}

func stripAnsiCodes(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return re.ReplaceAllString(s, "")
}

var maxTitleWidth int

func init() {
	for _, name := range shamsyMonths {
		y := 1400
		title := fmt.Sprintf("%s %d", name, y)
		width := len(title)
		width += 8 + 6
		if width > maxTitleWidth {
			maxTitleWidth = width
		}
	}
	if maxTitleWidth < 28 {
		maxTitleWidth = 28
	}
}

func printshamsyCalendar(jy, jm, highlight int, holidays map[string]string) {
	titleText := fmt.Sprintf("%s %d", shamsyMonths[jm-1], jy)
	totalPad := maxTitleWidth - len(titleText)
	leftPad := totalPad / 2
	rightPad := totalPad - leftPad
	head := fmt.Sprintf("%s%s%s", strings.Repeat("=", leftPad), titleText, strings.Repeat("=", rightPad))
	fmt.Println(rgb(red, head))

	for _, wd := range weekDays {
		cell := fmt.Sprintf("%4s", wd)
		fmt.Print(rgb(green, cell))
	}
	fmt.Println()

	first := getFirstWeekday(jy, jm)
	currentPos := first
	fmt.Print(strings.Repeat("    ", first))

	days := shamsyMonthDays(jy, jm)
	for d := 1; d <= days; d++ {
		key := fmt.Sprintf("%d-%02d-%02d", jy, jm, d)

		gy, gm, gd := shamsyToGregorian(jy, jm, d)
		weekday := time.Date(gy, time.Month(gm), gd, 0, 0, 0, 0, time.Local).Weekday()

		if d == highlight {
			cell := fmt.Sprintf("%2d", d)
			cell = fmt.Sprintf("%4s", cell)
			fmt.Print(rgb(yellow, cell))
		} else if _, ok := holidays[key]; ok {
			cell := fmt.Sprintf("%4s", fmt.Sprintf("%2d", d))
			fmt.Print(rgb(offday, cell))
		} else if weekday == time.Friday {
			cell := fmt.Sprintf("%4s", fmt.Sprintf("%2d", d))
			fmt.Print(rgb(offday, cell))
		} else {
			cell := fmt.Sprintf("%4s", fmt.Sprintf("%2d", d))
			fmt.Print(rgb(blue, cell))
		}

		currentPos++
		if currentPos%7 == 0 {
			fmt.Println()
			currentPos = 0
		}
	}

	if currentPos != 0 {
		for i := currentPos; i < 7; i++ {
			fmt.Print("    ")
		}
		fmt.Println()
	}
	fmt.Println("\n")
}

func printHolidaysOfMonth(jy, jm int, holidays map[string]string) {
	fmt.Println("\n📌 Holidays in this month:")
	found := false
	for d := 1; d <= shamsyMonthDays(jy, jm); d++ {
		key := fmt.Sprintf("%d-%02d-%02d", jy, jm, d)
		if desc, ok := holidays[key]; ok {
			fmt.Printf("- %02d %s: %s\n", d, shamsyMonths[jm-1], desc)
			found = true
		}
	}
	if !found {
		fmt.Println("No holidays in this month.")
	}
}

func main() {
	args := os.Args[1:]
	var jy, jm, highlight int
	var holidays map[string]string
	var err error

	switch len(args) {
	case 0:
		now := time.Now()
		gy, gm, gd := now.Date()
		jy, jm, highlight = gregorianToshamsy(gy, int(gm), gd)
		holidays, err = fetchHolidays(jy)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching holidays: %v\n", err)
			os.Exit(1)
		}
		printshamsyCalendar(jy, jm, highlight, holidays)

	case 1:
		y, err := strconv.Atoi(args[0])
		if err != nil || y < 1 {
			fmt.Println("Invalid year argument.")
			os.Exit(1)
		}
		holidays, err = fetchHolidays(y)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching holidays: %v\n", err)
			os.Exit(1)
		}
		for row := 0; row < 3; row++ {
			var monthLines [4][]string
			maxLines := 0
			for col := 0; col < 4; col++ {
				m := row*4 + col + 1
				origStdout := os.Stdout
				r, w, _ := os.Pipe()
				os.Stdout = w
				printshamsyCalendar(y, m, 0, holidays)
				w.Close()
				os.Stdout = origStdout
				buf := make([]byte, 4096)
				n, _ := r.Read(buf)
				lines := strings.Split(string(buf[:n]), "\n")
				for len(lines) > 0 && lines[len(lines)-1] == "" {
					lines = lines[:len(lines)-1]
				}
				for i, line := range lines {
					if i == 0 {
						continue
					}
					visibleLine := stripAnsiCodes(line)
					visibleLine = strings.TrimSpace(visibleLine)
					visibleLen := len(visibleLine)
					if visibleLen == 0 {
						lines[i] = strings.Repeat(" ", maxTitleWidth)
					} else if len(stripAnsiCodes(line)) < maxTitleWidth {
						rightPad := maxTitleWidth - len(stripAnsiCodes(line))
						lines[i] = line + strings.Repeat(" ", rightPad)
					}
				}
				monthLines[col] = lines
				if len(lines) > maxLines {
					maxLines = len(lines)
				}
			}
			for col := 0; col < 4; col++ {
				for len(monthLines[col]) < maxLines {
					monthLines[col] = append(monthLines[col], strings.Repeat(" ", maxTitleWidth))
				}
			}
			for i := 0; i < maxLines; i++ {
				for col := 0; col < 4; col++ {
					fmt.Print(monthLines[col][i])
					fmt.Print("    ")
				}
				fmt.Println()
			}
			fmt.Println()
		}

	case 2, 3:
		y, err1 := strconv.Atoi(args[0])
		m, err2 := strconv.Atoi(args[1])
		showHolidays := false
		if len(args) == 3 && args[2] == "--show-holidays" {
			showHolidays = true
		}
		if err1 != nil || err2 != nil || y < 1 || m < 1 || m > 12 {
			fmt.Println("Invalid year or month argument.")
			os.Exit(1)
		}
		holidays, err = fetchHolidays(y)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching holidays: %v\n", err)
			os.Exit(1)
		}
		printshamsyCalendar(y, m, 0, holidays)
		if showHolidays {
			printHolidaysOfMonth(y, m, holidays)
		}

	default:
		fmt.Println("Usage: program [year [month [--show-holidays]]]")
		os.Exit(1)
	}
}
