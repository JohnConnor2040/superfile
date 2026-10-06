package internal

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yorukot/superfile/src/internal/mouse"
)

// leftClickAt builds a left button press at the given terminal coordinates.
func leftClickAt(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// ctrlClickAt builds a ctrl modified left button press.
func ctrlClickAt(x, y int) tea.MouseClickMsg {
	click := leftClickAt(x, y)
	click.Mod = tea.ModCtrl
	return click
}

// findCell scans for a published hit-test region and returns terminal
// coordinates inside it, so that tests exercise the same lookup a real click
// goes through instead of hardcoding geometry.
func findCell(m *model, kind mouse.TargetKind, itemIndex int) (int, int, bool) {
	// Publishing regions happens during render, so render once before scanning.
	m.viewContent()

	for y := range m.fullHeight {
		for x := range m.fullWidth {
			target := m.mouseTargetAt(x, y)
			if target.Kind == kind && target.ItemIndex == itemIndex {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// mustFindCell is findCell for tests that require the region to be on screen.
func mustFindCell(t *testing.T, m *model, kind mouse.TargetKind, itemIndex int) (int, int) {
	t.Helper()

	x, y, ok := findCell(m, kind, itemIndex)
	require.True(t, ok, "no published region for kind %v item %d", kind, itemIndex)
	return x, y
}

// dirWithFiles builds a temp directory holding count files plus one subdirectory.
func dirWithFiles(t *testing.T, count int) string {
	t.Helper()

	dir := t.TempDir()
	for i := range count {
		name := filepath.Join(dir, "file"+string(rune('a'+i))+".txt")
		require.NoError(t, os.WriteFile(name, []byte("data"), 0o600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o755))
	return dir
}

// TestClickMovesFilePanelCursor checks that a single click focuses the panel and
// places the cursor on the clicked row.
func TestClickMovesFilePanelCursor(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)

	// Start on the first entry, then click a different one.
	require.Equal(t, 0, m.getFocusedFilePanel().GetCursor())

	const wantIndex = 3
	x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, wantIndex)
	m.handleMouseMsg(leftClickAt(x, y))

	assert.Equal(t, nonePanelFocus, m.focusPanel)
	assert.Equal(t, wantIndex, m.getFocusedFilePanel().GetCursor())

	wanted := m.getFocusedFilePanel().GetElementAtIdx(wantIndex)
	assert.Equal(t, wanted.Location,
		m.getFocusedFilePanel().GetElementAtIdx(m.getFocusedFilePanel().GetCursor()).Location)
}

// TestClickOnFocusedPanelKeepsFocus guards against reusing the keyboard focus
// helpers, which toggle: clicking an already focused panel must not unfocus it.
func TestClickOnFocusedPanelKeepsFocus(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)

	x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, 2)
	m.handleMouseMsg(leftClickAt(x, y))
	require.Equal(t, nonePanelFocus, m.focusPanel)

	m.handleMouseMsg(leftClickAt(x, y))
	assert.Equal(t, nonePanelFocus, m.focusPanel,
		"clicking the focused panel again must not toggle focus away")
	assert.True(t, m.getFocusedFilePanel().IsFocused)
}

// TestCtrlClickTogglesSelection checks that ctrl click builds a selection
// without moving away from the clicked row.
func TestCtrlClickTogglesSelection(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	first := panel.GetElementAtIdx(0)
	third := panel.GetElementAtIdx(2)

	x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, 0)
	m.handleMouseMsg(ctrlClickAt(x, y))
	assert.True(t, panel.CheckSelected(first.Location))

	x, y = mustFindCell(t, m, mouse.TargetFilePanelItem, 2)
	m.handleMouseMsg(ctrlClickAt(x, y))
	assert.True(t, panel.CheckSelected(third.Location))
	assert.Equal(t, 2, panel.GetCursor(), "ctrl click should also move the cursor")

	// Clicking the same row again removes it from the selection.
	m.handleMouseMsg(ctrlClickAt(x, y))
	assert.False(t, panel.CheckSelected(third.Location))
}

// TestDoubleClickEntersDirectory checks that two quick clicks on a directory
// enter it, while a single click does not.
func TestDoubleClickEntersDirectory(t *testing.T) {
	dir := dirWithFiles(t, 3)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	dirIndex := panel.FindElementIndexByName("subdir")
	require.NotEqual(t, -1, dirIndex, "subdir should be listed")

	x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, dirIndex)

	// A single click only moves the cursor.
	m.handleMouseMsg(leftClickAt(x, y))
	assert.Equal(t, dir, panel.Location, "a single click must not enter the directory")

	m.handleMouseMsg(leftClickAt(x, y))
	assert.Equal(t, filepath.Join(dir, "subdir"), panel.Location,
		"a double click should enter the directory")
}

// TestClicksSeparatedInTimeAreNotDoubleClicks makes sure the double click window
// is actually respected, so two deliberate clicks do not enter a directory.
func TestClicksSeparatedInTimeAreNotDoubleClicks(t *testing.T) {
	dir := dirWithFiles(t, 3)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	dirIndex := panel.FindElementIndexByName("subdir")
	require.NotEqual(t, -1, dirIndex)

	x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, dirIndex)
	m.handleMouseMsg(leftClickAt(x, y))

	// Pretend the previous click happened long ago.
	m.lastLeftClick.at = m.lastLeftClick.at.Add(-doubleClickWindow * 2)
	m.handleMouseMsg(leftClickAt(x, y))

	assert.Equal(t, dir, panel.Location)
}

// TestClickOnUnfocusedPanelFocusesIt checks that clicking a panel that is not
// focused hands focus over to it and takes focus away from the old one.
func TestClickOnUnfocusedPanelFocusesIt(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)

	// A newly created panel has no entries loaded yet, which is enough here:
	// the click is on the panel that does have entries.
	_, err := m.fileModel.CreateNewFilePanel(dir)
	require.NoError(t, err)
	require.Equal(t, 2, m.fileModel.PanelCount())

	m.fileModel.SetFocusedPanelIndex(1)
	require.True(t, m.fileModel.FilePanels[1].IsFocused)

	x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, 1)
	m.handleMouseMsg(leftClickAt(x, y))

	assert.Equal(t, 0, m.fileModel.FocusedPanelIndex)
	assert.True(t, m.fileModel.FilePanels[0].IsFocused)
	assert.False(t, m.fileModel.FilePanels[1].IsFocused,
		"the previously focused panel must be unfocused")
	assert.Equal(t, 1, m.getFocusedFilePanel().GetCursor())
}

// TestSetFocusedPanelIndexIsIdempotent covers the API behind click to focus,
// including the cases a stale hit-test region could produce.
func TestSetFocusedPanelIndexIsIdempotent(t *testing.T) {
	dir := dirWithFiles(t, 3)
	m := defaultTestModel(dir)
	require.Equal(t, 1, m.fileModel.PanelCount())

	// Focusing the only panel is a no-op rather than an error.
	m.fileModel.SetFocusedPanelIndex(0)
	assert.Equal(t, 0, m.fileModel.FocusedPanelIndex)
	assert.True(t, m.fileModel.FilePanels[0].IsFocused)

	// Out of range indices are rejected instead of panicking or corrupting the
	// focus state.
	m.fileModel.SetFocusedPanelIndex(1)
	m.fileModel.SetFocusedPanelIndex(-1)
	assert.Equal(t, 0, m.fileModel.FocusedPanelIndex)
	assert.True(t, m.fileModel.FilePanels[0].IsFocused)
}

// TestClickOnFooterFocusesFooterPanel checks that the footer panels can be
// reached by clicking them.
func TestClickOnFooterFocusesFooterPanel(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModelWithFooterAndFilePreview(dir)
	require.True(t, m.toggleFooter, "footer should be enabled for this test")

	m.viewContent()

	// A row inside the footer, left of the metadata panel.
	m.handleMouseMsg(leftClickAt(1, m.mainPanelHeight+1))
	assert.Equal(t, processBarFocus, m.focusPanel)

	m.handleMouseMsg(leftClickAt(m.processBarModel.GetWidth()+1, m.mainPanelHeight+1))
	assert.Equal(t, metadataFocus, m.focusPanel)
}

// TestClickOnBottomBorderDoesNotFocusFooter guards the footer band against
// running one row too far, where the bottom border lives.
func TestClickOnBottomBorderDoesNotFocusFooter(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModelWithFooterAndFilePreview(dir)
	require.True(t, m.toggleFooter, "footer should be enabled for this test")

	m.viewContent()

	// The last row is the bottom border, below the footer. The column is chosen
	// outside the sidebar so that the sidebar branch cannot claim the click.
	m.handleMouseMsg(leftClickAt(m.fullWidth-1, m.fullHeight-1))

	assert.Equal(t, nonePanelFocus, m.focusPanel,
		"the bottom border is not part of the footer")
	assert.True(t, m.getFocusedFilePanel().IsFocused)
}

// TestClickSidebarDirectoryFocusesAndNavigates checks that clicking a sidebar
// entry navigates the file panel and leaves the cursor on the chosen entry.
//
// Navigating hands focus back to the file panel, which is what the keyboard
// path does too, so the click must reproduce that rather than keep the sidebar
// focused.
func TestClickSidebarDirectoryFocusesAndNavigates(t *testing.T) {
	dir := dirWithFiles(t, 3)
	m := defaultTestModel(dir)
	m.sidebarModel.UpdateDirectories()
	m.viewContent()

	// Entry 0 is the directory the file panel already shows, so click entry 1
	// to be sure the click navigates somewhere new.
	const itemIndex = 1
	x, y := mustFindCell(t, m, mouse.TargetSidebarDirectory, itemIndex)

	m.handleMouseMsg(leftClickAt(x, y))

	assert.Equal(t, itemIndex, m.sidebarModel.GetCursor())
	assert.NotEqual(t, dir, m.getFocusedFilePanel().Location,
		"clicking a sidebar entry should navigate the file panel")
	assert.Equal(t, m.getFocusedFilePanel().Location, m.sidebarModel.GetCurrentDirectoryLocation(),
		"the sidebar cursor should stay on the directory that was navigated to")
	assert.Equal(t, nonePanelFocus, m.focusPanel)
	assert.True(t, m.getFocusedFilePanel().IsFocused)
}

// TestClickOnSidebarBackgroundOnlyFocuses checks that clicking the sidebar away
// from any entry focuses it without navigating or moving the cursor.
func TestClickOnSidebarBackgroundOnlyFocuses(t *testing.T) {
	dir := dirWithFiles(t, 3)
	m := defaultTestModel(dir)
	m.sidebarModel.UpdateDirectories()
	m.viewContent()

	dirBefore := m.sidebarModel.GetCurrentDirectoryLocation()
	locationBefore := m.getFocusedFilePanel().Location

	// Bottom left of the sidebar, past the last directory entry.
	m.handleMouseMsg(leftClickAt(1, m.fullHeight-1))

	assert.Equal(t, sidebarFocus, m.focusPanel)
	assert.Equal(t, dirBefore, m.sidebarModel.GetCurrentDirectoryLocation())
	assert.Equal(t, locationBefore, m.getFocusedFilePanel().Location)
}

// TestWheelStillScrolls checks the wheel path still works after the mouse
// handling was reworked onto typed messages.
func TestWheelStillScrolls(t *testing.T) {
	dir := dirWithFiles(t, 40)
	m := defaultTestModel(dir)

	start := m.getFocusedFilePanel().GetCursor()
	m.handleMouseMsg(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	assert.NotEqual(t, start, m.getFocusedFilePanel().GetCursor())

	afterDown := m.getFocusedFilePanel().GetCursor()
	m.handleMouseMsg(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	assert.NotEqual(t, afterDown, m.getFocusedFilePanel().GetCursor())
}

// TestWheelMotionEventsScroll covers terminals that report continued scrolling
// as motion events carrying the wheel button.
func TestWheelMotionEventsScroll(t *testing.T) {
	dir := dirWithFiles(t, 40)
	m := defaultTestModel(dir)

	start := m.getFocusedFilePanel().GetCursor()
	m.handleMouseMsg(tea.MouseMotionMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	assert.NotEqual(t, start, m.getFocusedFilePanel().GetCursor())
}
