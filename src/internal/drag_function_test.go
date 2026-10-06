package internal

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yorukot/superfile/src/internal/mouse"
	"github.com/yorukot/superfile/src/internal/ui/filepanel"
)

// mouseMotionAt builds a motion event with the left button held, which is how a
// terminal reports a drag.
func mouseMotionAt(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// mouseReleaseAt builds a left button release.
func mouseReleaseAt(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// startDrag presses on one cell and moves the pointer to another with the button
// held, which is everything a drag is up to the release.
//
// It moves in more than one step on purpose, so that a drag is shown to survive
// being reported in several motion events rather than one jump, and it insists
// the drag really started, so that a test cannot pass by never dragging at all.
func startDrag(t *testing.T, m *model, fromX, fromY, toX, toY int) {
	t.Helper()

	m.handleMouseMsg(leftClickAt(fromX, fromY))
	m.handleMouseMsg(mouseMotionAt(fromX+1, fromY))
	m.handleMouseMsg(mouseMotionAt(toX, toY))

	require.True(t, m.drag.active, "the pointer travelled far enough, so this should be a drag")
	require.Equal(t, m.mouseTargetAt(toX, toY), m.drag.target,
		"the drop target should be whatever is under the pointer")
}

// runCmd runs a returned command so the operation it starts actually happens.
func runCmd(cmd tea.Cmd) {
	if cmd != nil {
		cmd()
	}
}

// fileRow returns the element index of the first plain file in a panel.
//
// Rows are looked up rather than hardcoded because a panel sorts directories
// first, so index zero is the directory rather than a file.
func fileRow(panel *filepanel.Model) int {
	for i := range panel.ElemCount() {
		if !panel.GetElementAtIdx(i).Directory {
			return i
		}
	}
	return -1
}

// directoryRow returns the element index of the first directory in a panel.
func directoryRow(panel *filepanel.Model) int {
	for i := range panel.ElemCount() {
		if panel.GetElementAtIdx(i).Directory {
			return i
		}
	}
	return -1
}

// lastRow returns the element index of the last entry in a panel.
func lastRow(panel *filepanel.Model) int {
	return panel.ElemCount() - 1
}

func mustFileRow(t *testing.T, panel *filepanel.Model) int {
	t.Helper()

	row := fileRow(panel)
	require.GreaterOrEqual(t, row, 0, "the panel should contain a plain file")
	return row
}

func mustDirectoryRow(t *testing.T, panel *filepanel.Model) int {
	t.Helper()

	row := directoryRow(panel)
	require.GreaterOrEqual(t, row, 0, "the panel should contain a directory")
	return row
}

// TestDragMovesFileIntoDirectory is the main path: press a file, drag it onto a
// directory row, release, and the file ends up inside that directory.
func TestDragMovesFileIntoDirectory(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	// The last file is dragged up onto the directory, which is the first row, so
	// the pointer genuinely travels.
	srcRow := lastRow(panel)
	moved := panel.GetElementAtIdx(srcRow).Location
	destRow := mustDirectoryRow(t, panel)
	dest := panel.GetElementAtIdx(destRow).Location

	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)
	toX, toY := mustFindCell(t, m, mouse.TargetFilePanelItem, destRow)

	startDrag(t, m, fromX, fromY, toX, toY)
	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))

	require.NotNil(t, cmd, "dropping on a directory should start a move")
	runCmd(cmd)

	assert.Eventually(t, func() bool {
		return fileExists(filepath.Join(dest, filepath.Base(moved)))
	}, DefaultTestTimeout, DefaultTestTick,
		"the dragged file should end up inside the dropped directory")
	assert.NoFileExists(t, moved, "the file should have left where it was")
}

// TestDragIntoOtherPanelUsesThatPanelsDirectory checks that the destination comes
// from the panel the pointer is over, not the panel the drag started in.
func TestDragIntoOtherPanelUsesThatPanelsDirectory(t *testing.T) {
	dir := dirWithFiles(t, 5)
	other := t.TempDir()
	m := defaultTestModelWithFooterAndFilePreview(dir)
	// A second panel showing a different directory. It has no entries loaded,
	// which is all that is needed, since the drop lands on its area.
	_, err := m.fileModel.CreateNewFilePanel(other)
	require.NoError(t, err)
	require.Equal(t, 2, m.fileModel.PanelCount())
	m.viewContent()

	panel := &m.fileModel.FilePanels[0]
	srcRow := lastRow(panel)
	moved := panel.GetElementAtIdx(srcRow).Location
	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)

	// The area of the second panel means the directory that panel is showing.
	toX := m.fileModel.PanelOriginX(1) + 1
	toY := m.mainPanelHeight - 1
	require.Equal(t, mouse.TargetUnknown, m.mouseTargetAt(toX, toY).Kind)

	startDrag(t, m, fromX, fromY, toX, toY)
	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))

	require.NotNil(t, cmd, "the area around a panel is a drop target")
	runCmd(cmd)

	assert.Eventually(t, func() bool {
		return fileExists(filepath.Join(other, filepath.Base(moved)))
	}, DefaultTestTimeout, DefaultTestTick,
		"the file should have moved into the directory the second panel shows")
	assert.NoFileExists(t, moved)
}

// TestSmallMovementIsStillAClick checks that a press which jitters does not turn
// into a drag, because otherwise every slightly shaky click would move a file.
func TestSmallMovementIsStillAClick(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)

	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, mustDirectoryRow(t, m.getFocusedFilePanel()))

	m.handleMouseMsg(leftClickAt(fromX, fromY))
	m.handleMouseMsg(mouseMotionAt(fromX+1, fromY))

	assert.False(t, m.drag.active, "a one cell move is not a drag")

	cmd := m.handleMouseMsg(mouseReleaseAt(fromX+1, fromY))
	assert.Nil(t, cmd, "a click should not move anything")
	assert.Equal(t, 0, m.getFocusedFilePanel().GetCursor(),
		"a shaky click is still a click, so the cursor follows it")
}

// TestDragOntoFileDoesNothing checks that a file is not a place things can be
// moved into.
func TestDragOntoFileDoesNothing(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	srcRow := lastRow(panel)
	moved := panel.GetElementAtIdx(srcRow).Location
	fileIdx := mustFileRow(t, panel)

	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)
	toX, toY := mustFindCell(t, m, mouse.TargetFilePanelItem, fileIdx)

	startDrag(t, m, fromX, fromY, toX, toY)
	assert.Empty(t, m.drag.destLocation, "a file is not somewhere to drop")

	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))
	assert.Nil(t, cmd)
	assert.FileExists(t, moved, "nothing may be moved onto a file")
}

// TestDragIntoOwnDirectoryDoesNothing checks that dropping back into the directory
// a file is already in is not treated as a move, since there is nothing to do.
func TestDragIntoOwnDirectoryDoesNothing(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()
	m.viewContent()

	srcRow := lastRow(panel)
	moved := panel.GetElementAtIdx(srcRow).Location
	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)

	// Below the last entry is still the panel area, so it means the directory the
	// panel already shows.
	toX, toY := m.fullWidth/2, m.mainPanelHeight-1
	startDrag(t, m, fromX, fromY, toX, toY)
	require.Empty(t, m.drag.destLocation,
		"the panel already shows this directory, so there is nowhere new to drop")

	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))
	assert.Nil(t, cmd)
	assert.FileExists(t, moved)
}

// TestDragDirectoryIntoOwnParentIsRefused checks the destructive case is refused
// before anything is touched.
func TestDragDirectoryIntoOwnParentIsRefused(t *testing.T) {
	dir := dirWithFiles(t, 3)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	inner := filepath.Join(dir, "subdir", "inner")
	require.NoError(t, os.Mkdir(inner, 0o755))
	dirRow := mustDirectoryRow(t, panel)

	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, dirRow)
	m.viewContent()

	// The panel shows dir, so its area means dir itself, which is the parent of
	// subdir: moving a directory into its own parent would move it onto itself.
	toX, toY := m.fullWidth/2, m.mainPanelHeight-1
	startDrag(t, m, fromX, fromY, toX, toY)
	require.Empty(t, m.drag.destLocation, "a directory cannot be dropped into its own parent")

	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))
	assert.Nil(t, cmd)
	assert.DirExists(t, filepath.Join(dir, "subdir"))
	assert.DirExists(t, inner)
}

// TestDragIntoSidebarDirectory checks a sidebar row is a valid destination.
func TestDragIntoSidebarDirectory(t *testing.T) {
	dir := dirWithFiles(t, 5)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	m := defaultTestModel(dir)
	m.sidebarModel.UpdateDirectories()

	panel := m.getFocusedFilePanel()
	srcRow := lastRow(panel)
	moved := panel.GetElementAtIdx(srcRow).Location
	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)

	sidebarIndex := -1
	for i := range 40 {
		if m.sidebarModel.GetDirectoryLocation(i) == home {
			sidebarIndex = i
			break
		}
	}
	require.GreaterOrEqual(t, sidebarIndex, 0, "the home directory should be listed in the sidebar")
	toX, toY := mustFindCell(t, m, mouse.TargetSidebarDirectory, sidebarIndex)

	startDrag(t, m, fromX, fromY, toX, toY)
	require.Equal(t, home, m.drag.destLocation)

	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))
	require.NotNil(t, cmd, "a sidebar directory is a drop target")
	runCmd(cmd)

	assert.Eventually(t, func() bool {
		return fileExists(filepath.Join(home, filepath.Base(moved)))
	}, DefaultTestTimeout, DefaultTestTick,
		"the dragged file should end up in the sidebar directory")
}

// TestEscapeCancelsDrag checks a drag can be abandoned, leaving everything as it
// was.
func TestEscapeCancelsDrag(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()
	srcRow := lastRow(panel)
	moved := panel.GetElementAtIdx(srcRow).Location
	destRow := mustDirectoryRow(t, panel)

	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)
	toX, toY := mustFindCell(t, m, mouse.TargetFilePanelItem, destRow)

	startDrag(t, m, fromX, fromY, toX, toY)
	require.Equal(t, destRow, panel.DropTarget(), "the hovered row should be marked")

	require.True(t, m.cancelDrag())
	assert.False(t, m.drag.active)
	assert.Equal(t, filepanel.NoDropTarget, panel.DropTarget(),
		"cancelling should take the drop highlight off")

	// The release after a cancel must not move anything either.
	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))
	assert.Nil(t, cmd)
	assert.FileExists(t, moved)
}

// TestReleaseWithoutDragDoesNothing checks the ordinary click and release path is
// untouched.
func TestReleaseWithoutDragDoesNothing(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)

	x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, mustFileRow(t, m.getFocusedFilePanel()))
	m.handleMouseMsg(leftClickAt(x, y))
	require.True(t, m.drag.pending, "a press on a row could still become a drag")

	cmd := m.handleMouseMsg(mouseReleaseAt(x, y))
	assert.Nil(t, cmd)
	assert.False(t, m.drag.pending, "the release should have ended the pending press")
	assert.Equal(t, filepanel.NoDropTarget, m.getFocusedFilePanel().DropTarget())
}

// TestDragOutsideAnyPanelIsRefused checks a release with nowhere valid to land does
// nothing.
func TestDragOutsideAnyPanelIsRefused(t *testing.T) {
	dir := dirWithFiles(t, 3)
	m := defaultTestModelWithFooterAndFilePreview(dir)
	m.viewContent()
	panel := m.getFocusedFilePanel()
	srcRow := lastRow(panel)
	moved := panel.GetElementAtIdx(srcRow).Location

	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)
	// The footer is below the panels and is not a drop target.
	toX, toY := m.fullWidth-1, m.mainPanelHeight+1

	startDrag(t, m, fromX, fromY, toX, toY)
	require.Empty(t, m.drag.destLocation, "the footer is not a drop target")

	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))
	assert.Nil(t, cmd)
	assert.FileExists(t, moved)
}

// TestDragCarriesWholeSelection checks that dragging from inside a selection
// moves everything that was selected, the same way a right click keeps it.
func TestDragCarriesWholeSelection(t *testing.T) {
	dir := dirWithFiles(t, 6)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	firstRow := mustFileRow(t, panel)
	secondRow := firstRow + 1
	require.Less(t, secondRow, panel.ElemCount())
	first := panel.GetElementAtIdx(firstRow).Location
	second := panel.GetElementAtIdx(secondRow).Location

	for _, idx := range []int{firstRow, secondRow} {
		x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, idx)
		m.handleMouseMsg(ctrlClickAt(x, y))
	}
	require.Equal(t, uint(2), panel.SelectedCount())
	m.viewContent()

	// Drag from the lower of the two selected rows up onto the directory.
	destRow := mustDirectoryRow(t, panel)
	dest := panel.GetElementAtIdx(destRow).Location
	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, secondRow)
	toX, toY := mustFindCell(t, m, mouse.TargetFilePanelItem, destRow)

	startDrag(t, m, fromX, fromY, toX, toY)
	require.ElementsMatch(t, []string{first, second}, m.drag.locations,
		"a drag from inside a selection carries the whole selection")

	cmd := m.handleMouseMsg(mouseReleaseAt(toX, toY))
	require.NotNil(t, cmd)
	runCmd(cmd)

	assert.Eventually(t, func() bool {
		return fileExists(filepath.Join(dest, filepath.Base(first))) &&
			fileExists(filepath.Join(dest, filepath.Base(second)))
	}, DefaultTestTimeout, DefaultTestTick,
		"the whole selection should have moved")
}

// TestDragOutsideSelectionCarriesOnlyThatEntry checks dragging from outside a
// selection carries only the pressed entry.
func TestDragOutsideSelectionCarriesOnlyThatEntry(t *testing.T) {
	dir := dirWithFiles(t, 6)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	selectedRow := mustFileRow(t, panel)
	x, y := mustFindCell(t, m, mouse.TargetFilePanelItem, selectedRow)
	m.handleMouseMsg(ctrlClickAt(x, y))
	require.Equal(t, uint(1), panel.SelectedCount())

	otherRow := lastRow(panel)
	other := panel.GetElementAtIdx(otherRow).Location
	m.viewContent()

	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, otherRow)
	destRow := mustDirectoryRow(t, panel)
	toX, toY := mustFindCell(t, m, mouse.TargetFilePanelItem, destRow)

	startDrag(t, m, fromX, fromY, toX, toY)
	require.Equal(t, []string{other}, m.drag.locations,
		"dragging an unselected row carries only that row")

	m.handleMouseMsg(mouseReleaseAt(toX, toY))
}

// TestDragDoesNotLeaveAPendingDoubleClick checks a drag cannot be mistaken for the
// first half of a double click, which would open the dragged file.
func TestDragDoesNotLeaveAPendingDoubleClick(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	srcRow := lastRow(panel)
	destRow := mustDirectoryRow(t, panel)
	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)
	toX, toY := mustFindCell(t, m, mouse.TargetFilePanelItem, destRow)

	m.handleMouseMsg(leftClickAt(fromX, fromY))
	require.NotEqual(t, leftClick{}, m.lastLeftClick,
		"the press should have been recorded as a click too")

	startDrag(t, m, fromX, fromY, toX, toY)
	assert.Equal(t, leftClick{}, m.lastLeftClick,
		"starting a drag should clear the recorded click, or the next click would open the file")

	// A second click right after the drag must therefore not be a double click.
	m.handleMouseMsg(mouseReleaseAt(toX, toY))
	m.handleMouseMsg(leftClickAt(fromX, fromY))
	assert.Equal(t, mouse.Target{
		Kind:       mouse.TargetFilePanelItem,
		PanelIndex: 0,
		ItemIndex:  srcRow,
	}, m.lastLeftClick.target,
		"the click after a drag should record as a fresh first click, not a second half")
}

// TestDropTargetFollowsThePointerAndClears checks the marked row tracks the
// pointer and disappears as soon as it leaves somewhere valid.
func TestDropTargetFollowsThePointerAndClears(t *testing.T) {
	dir := dirWithFiles(t, 5)
	m := defaultTestModel(dir)
	panel := m.getFocusedFilePanel()

	srcRow := lastRow(panel)
	destRow := mustDirectoryRow(t, panel)
	dest := panel.GetElementAtIdx(destRow).Location

	fromX, fromY := mustFindCell(t, m, mouse.TargetFilePanelItem, srcRow)
	toX, toY := mustFindCell(t, m, mouse.TargetFilePanelItem, destRow)

	startDrag(t, m, fromX, fromY, toX, toY)
	require.Equal(t, destRow, panel.DropTarget())
	require.Equal(t, dest, m.drag.destLocation)

	// Moving onto a plain file leaves nowhere valid to drop.
	fileIdx := mustFileRow(t, panel)
	fx, fy := mustFindCell(t, m, mouse.TargetFilePanelItem, fileIdx)
	m.handleMouseMsg(mouseMotionAt(fx, fy))

	require.True(t, m.drag.active, "the drag is still active, only the target moved")
	assert.Equal(t, filepanel.NoDropTarget, panel.DropTarget(),
		"a file is not a drop target, so nothing should be marked")
	assert.Empty(t, m.drag.destLocation)
}
