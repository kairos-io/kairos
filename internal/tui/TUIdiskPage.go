package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jaypipes/ghw/pkg/block"
	"github.com/jaypipes/ghw/pkg/option"
)

type diskStruct struct {
	id   int
	name string
	size string
}

// Disk Selection Page
type diskSelectionPage struct {
	disks  []diskStruct
	cursor int
	offset int // index of the first visible disk in the scroll window
}

// diskPageHeaderLines is the number of fixed lines the page renders before the
// disk list: the "Select target disk" prompt + blank line, and the WARNING line
// + blank line.
const diskPageHeaderLines = 4

// visibleRows is the number of rows reserved by Model.View for page content
// (height minus this value gives the usable content area).
const visibleRows = 10

// visibleCount returns how many disk rows fit in the available vertical space.
// It mirrors the content budget applied by Model.View (height-visibleRows) and
// reserves two lines for the scroll indicators when the list overflows.
func (p *diskSelectionPage) visibleCount() int {
	_, height := effectiveSize(mainModel.width, mainModel.height)
	// Model.View slices each page's content to height-visibleRows lines.
	avail := height - visibleRows - diskPageHeaderLines
	if len(p.disks) > avail {
		// Reserve room for the top/bottom "..." scroll indicators.
		avail -= 2
	}
	if avail < 1 {
		avail = 1
	}
	return avail
}

// clampOffset keeps the scroll window so the cursor stays visible and the
// offset never runs past the end of the list.
func (p *diskSelectionPage) clampOffset() {
	vc := p.visibleCount()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+vc {
		p.offset = p.cursor - vc + 1
	}
	maxOffset := len(p.disks) - vc
	if maxOffset < 0 {
		maxOffset = 0
	}
	if p.offset > maxOffset {
		p.offset = maxOffset
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func newDiskSelectionPage() *diskSelectionPage {
	bl, err := block.New(option.WithDisableTools(), option.WithNullAlerter())
	if err != nil {
		fmt.Printf("Error initializing block device info: %v\n", err)
		return nil
	}
	var disks []diskStruct

	const minDiskSizeBytes = 1 * 1024 * 1024 * 1024 // 1 GiB
	excludedDevicePrefixes := []string{"loop", "ram", "sr", "zram"}

	for _, disk := range bl.Disks {
		// Check if the device name starts with any excluded prefix
		excluded := false
		for _, prefix := range excludedDevicePrefixes {
			if strings.HasPrefix(disk.Name, prefix) {
				excluded = true
				break
			}
		}
		if excluded || disk.SizeBytes < minDiskSizeBytes {
			continue // Skip excluded devices and disks smaller than the minimum size
		}
		disks = append(disks, diskStruct{name: filepath.Join("/dev", disk.Name), size: fmt.Sprintf("%.2f GiB", float64(disk.SizeBytes)/float64(1024*1024*1024)), id: len(disks)})
	}

	return &diskSelectionPage{
		disks:  disks,
		cursor: 0,
	}
}

func (p *diskSelectionPage) Init() tea.Cmd {
	return nil
}

func (p *diskSelectionPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
			p.clampOffset()
		case "down", "j":
			if p.cursor < len(p.disks)-1 {
				p.cursor++
			}
			p.clampOffset()
		case "enter":
			// Store selected disk in mainModel
			if p.cursor >= 0 && p.cursor < len(p.disks) {
				mainModel.disk = p.disks[p.cursor].name
			}
			// Go to confirmation page
			return p, func() tea.Msg { return GoToPageMsg{PageID: "install_options"} }
		}
	}
	return p, nil
}

func (p *diskSelectionPage) View() string {
	s := "Select target disk for installation:\n\n"
	s += "WARNING: All data on the selected disk will be DESTROYED!\n\n"

	p.clampOffset()
	vc := p.visibleCount()
	start := p.offset
	end := start + vc
	if end > len(p.disks) {
		end = len(p.disks)
	}

	indicatorStyle := lipgloss.NewStyle().Foreground(kairosText)
	if start > 0 {
		s += indicatorStyle.Render("  ... more above") + "\n"
	}
	for i := start; i < end; i++ {
		disk := p.disks[i]
		cursor := " "
		if p.cursor == i {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		s += fmt.Sprintf("%s %s (%s)\n", cursor, disk.name, disk.size)
	}
	if end < len(p.disks) {
		s += indicatorStyle.Render("  ... more below") + "\n"
	}

	return s
}

func (p *diskSelectionPage) Title() string {
	return "Disk Selection"
}

func (p *diskSelectionPage) Help() string {
	return genericNavigationHelp
}

func (p *diskSelectionPage) ID() string { return "disk_selection" }
