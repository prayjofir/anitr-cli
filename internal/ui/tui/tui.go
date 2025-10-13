package tui

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/axrona/anitr-cli/internal"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

var (
	ErrQuit   = errors.New("quit requested")
	ErrGoBack = errors.New("go back requested")
	
	// Global spinner state
	currentSpinnerLabel string
	spinnerActive       bool
)

// Stil ve renkler
var (
	highlightFgColor = "#e45cc0"
	normalFgColor    = "#aabbcc"
	highlightColor   = "#e45cc0"
	filterInputFg    = "#8bb27f"
	filterCursorFg   = "#c4b48b"
	inputPromptFg    = "#c4b48b"
	inputTextFg      = "#aabbcc"
	inputCursorFg    = "#c4b48b"
	selectionMark    = "» "

	pinkHighlight = lipgloss.NewStyle().Foreground(lipgloss.Color(highlightColor))

	filterInputStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(filterInputFg)).
				Bold(true)

	highlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(highlightFgColor)).
			Bold(true).
			Padding(0, 1)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(normalFgColor)).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#444")).
			Italic(true)
)

// Spinner modeli
type SpinnerModel struct {
	spinner  spinner.Model
	label    string
	quitting bool
}

func ShowSpinner(label string, done chan struct{}) {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#e45cc0"))

	// Global state'i güncelle
	currentSpinnerLabel = label
	spinnerActive = true

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			fmt.Printf("\r✔ %s\n", currentSpinnerLabel)
			spinnerActive = false
			return
		case <-ticker.C:
			s, _ = s.Update(spinner.TickMsg{})
			fmt.Printf("\r%s %s", s.View(), currentSpinnerLabel)
		}
	}
}

// UpdateSpinnerMessage, aktif spinner'ın mesajını günceller
func UpdateSpinnerMessage(newLabel string) {
	if spinnerActive {
		currentSpinnerLabel = newLabel
	}
}

func (m SpinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m SpinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m SpinnerModel) View() string {
	if m.quitting {
		return ""
	}
	return fmt.Sprintf("%s %s", m.spinner.View(), m.label)
}

// Hatayı pastel kırmızı kutu içinde gösterir ve programı sonlandırır
func ShowErrorBox(message string) {
	// Pastel kırmızı kutu stili
	errorBox := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ff7f7f")). // pastel kırmızı
		Background(lipgloss.Color("#1c1c1c")). // koyu arka plan
		Bold(true).
		Padding(1, 2).
		Margin(1, 0).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#ff5f5f")) // kutu sınırı

	// Tam hata mesajını göster
	fullMessage := "❌ Hata: " + message

	// Kutunun içine render et
	fmt.Println(errorBox.Render(fullMessage))
}

// Tek seçimli list item
type listItem string

func (i listItem) Title() string       { return string(i) }
func (i listItem) Description() string { return "" }
func (i listItem) FilterValue() string { return string(i) }

// Çoklu seçim için checkbox item
type checkboxItem struct {
	TitleStr string
	Selected bool
}

// Genel ayırıcı item - her türlü separator için kullanılabilir
type separatorItem struct {
	Text string
}

func (i separatorItem) Title() string {
	return i.Text
}
func (i separatorItem) Description() string { return "" }
func (i separatorItem) FilterValue() string { return "" }

// Bir item'ın separator olup olmadığını kontrol eden yardımcı fonksiyon
func isSeparator(item list.Item) bool {
	_, isSeasonSeparator := item.(seasonSeparatorItem)
	_, isGeneralSeparator := item.(separatorItem)
	return isSeasonSeparator || isGeneralSeparator
}

// Sezon ayırıcısı için özel item (geriye uyumluluk için)
type seasonSeparatorItem struct {
	SeasonNumber int
}

func (i seasonSeparatorItem) Title() string {
	return fmt.Sprintf("────────── %d. Sezon ──────────", i.SeasonNumber)
}
func (i seasonSeparatorItem) Description() string { return "" }
func (i seasonSeparatorItem) FilterValue() string { return "" }

// Bölüm listesini sezonlara göre grupla ve ayırıcılar ekle
func processEpisodesWithSeparators(episodes []string, skipSeasonSeparators bool, skipAllSeparators bool) []string {
	if len(episodes) == 0 {
		return episodes
	}

	// Eğer tüm separator'lar atlanacaksa, orijinal listeyi döndür
	if skipAllSeparators {
		return episodes
	}

	// Önce bu listenin gerçekten bölüm listesi olup olmadığını kontrol et
	seasonRegex := regexp.MustCompile(`(\d+)\. Sezon, (\d+)\. Bölüm`)
	episodeCount := 0
	for _, episode := range episodes {
		if seasonRegex.MatchString(episode) {
			episodeCount++
		}
	}

	// Eğer hiç bölüm formatında string yoksa veya sezon ayırıcıları atlanacaksa, orijinal listeyi döndür
	if episodeCount == 0 || skipSeasonSeparators {
		return episodes
	}

	// Sezon numaralarını çıkar ve grupla
	seasonMap := make(map[int][]string)

	for _, episode := range episodes {
		matches := seasonRegex.FindStringSubmatch(episode)
		if len(matches) >= 3 {
			seasonNum, err := strconv.Atoi(matches[1])
			if err == nil {
				seasonMap[seasonNum] = append(seasonMap[seasonNum], episode)
			}
		} else {
			// Eğer format uymazsa orijinal listeye ekle
			seasonMap[0] = append(seasonMap[0], episode)
		}
	}

	// Sezon numaralarını sırala
	var seasons []int
	for season := range seasonMap {
		seasons = append(seasons, season)
	}
	sort.Ints(seasons)

	// Yeni listeyi oluştur
	var result []string
	for _, season := range seasons {
		if season > 0 {
			// Sezon ayırıcısı ekle
			separator := fmt.Sprintf("────────── %d. Sezon ──────────", season)
			result = append(result, separator)
			// Bölümleri ekle
			result = append(result, seasonMap[season]...)
		} else if season == 0 {
			// Sezon 0 (format uymayanlar) - ayırıcı olmadan ekle
			result = append(result, seasonMap[season]...)
		}
	}

	return result
}

func (i checkboxItem) Title() string       { return i.TitleStr }
func (i checkboxItem) Description() string { return "" }
func (i checkboxItem) FilterValue() string { return i.TitleStr }

// Render delegate
type slimDelegate struct {
	list.DefaultDelegate
}

func (d slimDelegate) Height() int  { return 1 }
func (d slimDelegate) Spacing() int { return 0 }
func (d slimDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	title := ""
	isSeasonSeparator := false

	if li, ok := item.(listItem); ok {
		// Tek seçimli item
		title = li.Title()
	} else if ci, ok := item.(checkboxItem); ok {
		// Çoklu seçimli checkbox item
		check := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666")). // seçilmemiş gri
			Italic(true).                       // italik
			Render("[ ] ")

		if ci.Selected {
			check = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e45cc0")). // seçili pembe
				Bold(true).                            // kalın
				Italic(true).                          // italik
				Render("[x] ")
		}

		title = check + ci.Title()
	} else if si, ok := item.(seasonSeparatorItem); ok {
		// Sezon ayırıcısı
		title = si.Title()
		isSeasonSeparator = true
	} else if sep, ok := item.(separatorItem); ok {
		// Genel ayırıcı
		title = sep.Title()
		isSeasonSeparator = true // Aynı rendering kullan
	} else {
		title = "???"
	}

	// Sezon ayırıcısı için özel rendering
	if isSeasonSeparator {
		line := headerStyle.Render(title)
		fmt.Fprint(w, line)
		return
	}

	// Seçili item için prefix
	isSelected := index == m.Index()
	prefix := "  "
	if isSelected {
		prefix = selectionMark
	}

	// Başlığı truncate et
	availableWidth := m.Width() - lipgloss.Width(prefix) - 4
	displayTitle := truncate.StringWithTail(title, uint(availableWidth), "...")

	// Satır stili
	line := prefix + displayTitle
	if isSelected {
		line = highlightStyle.Render(line)
	} else {
		line = normalStyle.Render(line)
	}

	fmt.Fprint(w, line)
}

// Tek seçimli model
type SelectionListModel struct {
	list     list.Model
	quitting bool
	selected []string
	err      error
	width    int
}

func NewSelectionListModel(params internal.UiParams) SelectionListModel {
	// Bölüm listesini işle ve sezon ayırıcıları ekle
	processedList := processEpisodesWithSeparators(*params.List, params.SkipSeasonSeparators, params.SkipAllSeparators)
	items := make([]list.Item, len(processedList))

	for i, v := range processedList {
		// Sezon ayırıcısı mı kontrol et
		if strings.Contains(v, "──────") && strings.Contains(v, "Sezon") {
			// Sezon numarasını çıkar
			seasonRegex := regexp.MustCompile(`(\d+)\. Sezon`)
			matches := seasonRegex.FindStringSubmatch(v)
			if len(matches) >= 2 {
				if seasonNum, err := strconv.Atoi(matches[1]); err == nil {
					items[i] = seasonSeparatorItem{SeasonNumber: seasonNum}
				} else {
					items[i] = listItem(v)
				}
			} else {
				items[i] = listItem(v)
			}
		} else if strings.Contains(v, "──────") {
			// Genel ayırıcı
			items[i] = separatorItem{Text: v}
		} else {
			items[i] = listItem(v)
		}
	}

	const defaultWidth, defaultHeight = 48, 20
	l := list.New(items, slimDelegate{}, defaultWidth, defaultHeight)

	titleStyle := lipgloss.NewStyle().Align(lipgloss.Center).Bold(true)
	l.Title = titleStyle.Render(params.Label)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.FilterInput.Prompt = pinkHighlight.Render("🔍 Search: ")
	l.FilterInput.Placeholder = "Ara..."
	l.FilterInput.TextStyle = filterInputStyle
	l.FilterInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(filterCursorFg))

	// İlk seçilebilir itemi bul ve seç
	for i := 0; i < len(items); i++ {
		if _, ok := items[i].(seasonSeparatorItem); !ok {
			l.Select(i)
			break
		}
	}

	return SelectionListModel{list: l}
}

func (m SelectionListModel) Init() tea.Cmd { return nil }
func (m SelectionListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			// Wrap: en üstteyken yukarı basınca en alta git
			if m.list.Index() == 0 {
				items := m.list.Items()
				if len(items) > 0 {
					// En son seçilebilir itemi bul
					for i := len(items) - 1; i >= 0; i-- {
						if !isSeparator(items[i]) {
							m.list.Select(i)
							return m, nil
						}
					}
				}
			} else {
				// Yukarı git ama sezon ayırıcılarını atla
				items := m.list.Items()
				currentIndex := m.list.Index()
				for i := currentIndex - 1; i >= 0; i-- {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
				// Eğer yukarıda seçilebilir item yoksa en alta git
				for i := len(items) - 1; i >= 0; i-- {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
			}
		case "down", "j":
			// Wrap: en alttayken aşağı basınca en başa git
			items := m.list.Items()
			if len(items) > 0 && m.list.Index() == len(items)-1 {
				// İlk seçilebilir itemi bul
				for i := 0; i < len(items); i++ {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
			} else {
				// Aşağı git ama sezon ayırıcılarını atla
				currentIndex := m.list.Index()
				for i := currentIndex + 1; i < len(items); i++ {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
				// Eğer aşağıda seçilebilir item yoksa en başa git
				for i := 0; i < len(items); i++ {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
			}
		case "enter":
			if i, ok := m.list.SelectedItem().(listItem); ok {
				m.selected = []string{string(i)}
			}
			m.quitting = true
			return m, tea.Quit

		case "ctrl+c", "q":
			m.err = ErrQuit
			m.quitting = true
			return m, tea.Quit

		case "esc":
			m.selected = nil
			m.quitting = true
			m.err = ErrGoBack
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m SelectionListModel) View() string {
	if m.quitting {
		return ""
	}
	return m.list.View()
}

func SelectionList(params internal.UiParams) (string, error) {
	p := tea.NewProgram(NewSelectionListModel(params), tea.WithAltScreen())
	m, err := p.Run()
	if err != nil {
		return "", err
	}
	model := m.(SelectionListModel)
	if model.err != nil {
		return "", model.err
	}
	if len(model.selected) > 0 {
		return model.selected[0], nil
	}
	return "", nil
}

// Çoklu seçimli model
type MultiSelectionListModel struct {
	list     list.Model
	selected []string
	quitting bool
	err      error
	width    int
}

func NewMultiSelectionListModel(params internal.UiParams) MultiSelectionListModel {
	// Bölüm listesini işle ve sezon ayırıcıları ekle
	processedList := processEpisodesWithSeparators(*params.List, params.SkipSeasonSeparators, params.SkipAllSeparators)
	items := make([]list.Item, len(processedList))

	for i, v := range processedList {
		// Sezon ayırıcısı mı kontrol et
		if strings.Contains(v, "──────") && strings.Contains(v, "Sezon") {
			// Sezon numarasını çıkar
			seasonRegex := regexp.MustCompile(`(\d+)\. Sezon`)
			matches := seasonRegex.FindStringSubmatch(v)
			if len(matches) >= 2 {
				if seasonNum, err := strconv.Atoi(matches[1]); err == nil {
					items[i] = seasonSeparatorItem{SeasonNumber: seasonNum}
				} else {
					items[i] = checkboxItem{TitleStr: v}
				}
			} else {
				items[i] = checkboxItem{TitleStr: v}
			}
		} else if strings.Contains(v, "──────") {
			// Genel ayırıcı
			items[i] = separatorItem{Text: v}
		} else {
			items[i] = checkboxItem{TitleStr: v}
		}
	}

	const defaultWidth, defaultHeight = 48, 20
	l := list.New(items, slimDelegate{}, defaultWidth, defaultHeight)
	titleStyle := lipgloss.NewStyle().Align(lipgloss.Center).Bold(true)
	l.Title = titleStyle.Render(params.Label)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.FilterInput.Prompt = pinkHighlight.Render("🔍 Search: ")
	l.FilterInput.Placeholder = "Ara..."
	l.FilterInput.TextStyle = filterInputStyle
	l.FilterInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(filterCursorFg))

	// İlk seçilebilir itemi bul ve seç
	for i := 0; i < len(items); i++ {
		if _, ok := items[i].(seasonSeparatorItem); !ok {
			l.Select(i)
			break
		}
	}

	return MultiSelectionListModel{list: l}
}

func (m MultiSelectionListModel) Init() tea.Cmd { return nil }
func (m MultiSelectionListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			// Wrap: en üstteyken yukarı basınca en alta git
			if m.list.Index() == 0 {
				items := m.list.Items()
				if len(items) > 0 {
					// En son seçilebilir itemi bul
					for i := len(items) - 1; i >= 0; i-- {
						if !isSeparator(items[i]) {
							m.list.Select(i)
							return m, nil
						}
					}
				}
			} else {
				// Yukarı git ama sezon ayırıcılarını atla
				items := m.list.Items()
				currentIndex := m.list.Index()
				for i := currentIndex - 1; i >= 0; i-- {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
				// Eğer yukarıda seçilebilir item yoksa en alta git
				for i := len(items) - 1; i >= 0; i-- {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
			}
		case "down", "j":
			// Wrap: en alttayken aşağı basınca en başa git
			items := m.list.Items()
			if len(items) > 0 && m.list.Index() == len(items)-1 {
				// İlk seçilebilir itemi bul
				for i := 0; i < len(items); i++ {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
			} else {
				// Aşağı git ama sezon ayırıcılarını atla
				currentIndex := m.list.Index()
				for i := currentIndex + 1; i < len(items); i++ {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
				// Eğer aşağıda seçilebilir item yoksa en başa git
				for i := 0; i < len(items); i++ {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
			}
		case "tab", " ":
			items := m.list.Items()
			if ci, ok := items[m.list.Index()].(checkboxItem); ok {
				ci.Selected = !ci.Selected
				items[m.list.Index()] = ci
				m.list.SetItems(items)

				// Sonraki seçilebilir iteme git
				currentIndex := m.list.Index()
				for i := currentIndex + 1; i < len(items); i++ {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
				// Eğer aşağıda seçilebilir item yoksa en başa git
				for i := 0; i < len(items); i++ {
					if !isSeparator(items[i]) {
						m.list.Select(i)
						return m, nil
					}
				}
			}
		case "enter":
			selected := []string{}
			for _, it := range m.list.Items() {
				if ci, ok := it.(checkboxItem); ok && ci.Selected {
					selected = append(selected, ci.Title())
				}
			}
			m.selected = selected
			if len(selected) == 0 {
				if ci, ok := m.list.SelectedItem().(checkboxItem); ok {
					m.selected = []string{ci.Title()}
				}
			}
			m.quitting = true
			return m, tea.Quit
		case "ctrl+c", "q":
			m.err = ErrQuit
			m.quitting = true
			return m, tea.Quit

		case "esc":
			m.selected = nil
			m.quitting = true
			m.err = ErrGoBack
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m MultiSelectionListModel) View() string {
	if m.quitting {
		return ""
	}
	return m.list.View()
}

func MultiSelectList(params internal.UiParams) ([]string, error) {
	p := tea.NewProgram(NewMultiSelectionListModel(params), tea.WithAltScreen())
	m, err := p.Run()
	if err != nil {
		return nil, err
	}
	model := m.(MultiSelectionListModel)
	if model.err != nil {
		return nil, model.err
	}
	return model.selected, nil
}

// Kullanıcıdan giriş almak
type InputFromUserModel struct {
	textInput textinput.Model
	err       error
	quitting  bool
}

func NewInputFromUserModel(params internal.UiParams) InputFromUserModel {
	ti := textinput.New()
	ti.Placeholder = ""
	ti.Prompt = "🔍 " + params.Label + ": "
	ti.CharLimit = 256
	ti.Focus()
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(inputPromptFg)).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(inputTextFg))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(inputCursorFg))
	return InputFromUserModel{textInput: ti}
}

func (m InputFromUserModel) Init() tea.Cmd { return textinput.Blink }
func (m InputFromUserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if len(strings.TrimSpace(m.textInput.Value())) == 0 {
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case "ctrl+c":
			m.err = ErrQuit
			m.quitting = true
			return m, tea.Quit

		case "esc":
			m.err = ErrGoBack
			m.quitting = true
			return m, tea.Quit

		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m InputFromUserModel) View() string {
	if m.quitting {
		return ""
	}
	return lipgloss.NewStyle().Padding(0, 2).Render(m.textInput.View())
}

func InputFromUser(params internal.UiParams) (string, error) {
	p := tea.NewProgram(NewInputFromUserModel(params))
	m, err := p.Run()
	if err != nil {
		return "", err
	}
	model := m.(InputFromUserModel)
	if model.err != nil {
		return "", model.err
	}
	return model.textInput.Value(), nil
}
