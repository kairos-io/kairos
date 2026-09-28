package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kairos-io/kairos/v4/installer/internal/disks"
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

// scanDisks enumerates installable block devices on the host. It is a
// package-level var so tests can substitute a deterministic implementation
// (and so the disk-selection page can refresh its list on every Init without
// coupling to ghw directly).
var scanDisks = scanDisksGHW

// scanDisksGHW is the production disk scanner. Which disks are candidates is
// the installer's rule rather than this page's, so it lives in
// installer/internal/disks and every frontend gets the same answer; this only
// shapes it for the list.
func scanDisksGHW() ([]diskStruct, error) {
	found, err := disks.Scan()
	if err != nil {
		return nil, err
	}

	out := make([]diskStruct, 0, len(found))
	for i, d := range found {
		out = append(out, diskStruct{name: d.Path, size: d.Size, id: i})
	}

	return out, nil
}

func newDiskSelectionPage() *diskSelectionPage {
	found, err := scanDisks()
	if err != nil {
		fmt.Printf("Error initializing block device info: %v\n", err)
		return nil
	}
	return &diskSelectionPage{
		disks:  found,
		cursor: 0,
	}
}

// Init re-scans the host for available disks every time the page is entered.
// Prerequisites plugins (e.g. wipefs) can mutate disk state between page
// construction and the moment the user reaches disk selection, so the cached
// list from newDiskSelectionPage is not authoritative. Refreshing here keeps
// the user picking from what actually exists on disk right now.
//
// See kairos-io/kairos#4260.
func (p *diskSelectionPage) Init() tea.Cmd {
	found, err := scanDisks()
	if err != nil {
		if mainModel.log != nil {
			mainModel.log.Logger.Warn().Err(err).Msg("Failed to refresh disk list; keeping previous view")
		}
		return nil
	}
	p.disks = found
	// The previously-selected disk may no longer exist. Clamp the cursor into
	// the new range and reset the scroll window to keep the view sane.
	if p.cursor >= len(p.disks) {
		p.cursor = 0
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.offset = 0
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
	return genericNavigationHelp + " • ctrl+d: collect debug logs"
}

func (p *diskSelectionPage) ID() string { return "disk_selection" }
