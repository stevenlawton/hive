# Resizable Split Dividers — Design

**Date:** 2026-07-17
**Status:** Approved

## Problem

Workspace splits (`ctrl+space v` / `ctrl+space h`) always divide the tab area
equally among panes (`recalcWidths()` in `ui/splitpane.go` computes
`available / n`). There is no way to move the divider — e.g. to give a main
session two thirds of the screen and a helper session one third.

## Goals

- Move the divider between panes via keyboard (resize mode) and mouse drag.
- Works for both orientations (side-by-side and stacked) and any pane count.
- Sizes survive terminal window resizes proportionally.

## Non-goals

- Persisting custom sizes across hive restarts (no other layout state persists).
- Resizing in fullscreen mode (single pane — nothing to resize).
- Nested/mixed-orientation splits (SplitPane is single-orientation today).

## Sizing model: proportional weights

Each `Split` gains a `Ratio float64`. New splits get equal ratios; ratios are
normalized on use, so they need not sum to 1.

`recalcWidths()` changes from equal division to proportional distribution:

- Compute each pane's cell size as `round(available * ratio_i / sum(ratios))`,
  last pane takes the remainder (as today) so cells always sum exactly.
- Enforce a minimum pane size: **10 columns** (vertical) / **3 rows**
  (horizontal). Ratio adjustments that would push a pane below the minimum are
  clamped.
- `AddSplit` / `RemoveSplit` reset all ratios to equal (predictable, avoids
  weird inherited proportions).

Rejected alternatives:
- **Absolute cell sizes** — breaks on window resize; needs re-normalization
  anyway.
- **tmux-style divider offsets** — more clamping state, no benefit at the
  2–3 panes hive typically shows.

## Keyboard: resize mode (`ctrl+space s`)

New `ChordResizeMode` action mapped to `s` in `chord.go`. The model enters a
resize state (a bool/mode field on `model`):

- **Arrow keys** nudge the divider between the focused pane and its *next*
  neighbour; the last pane adjusts against its *previous* neighbour.
  Left/right for vertical splits, up/down for horizontal. Off-axis arrows are
  ignored. Direction is literal: the arrow moves the divider that way —
  right/down grows the focused pane when its neighbour is after it, shrinks it
  when adjusting against a previous neighbour.
- Each nudge shifts the divider by **2 cells** worth of ratio.
- **`=`** re-equalizes all panes.
- **Esc or Enter** exits resize mode. Any other key is ignored (stays in
  mode) so a stray keystroke doesn't leak into the terminal pane.
- While in resize mode, keys are consumed by the mode — nothing is forwarded
  to the embedded tmux session.
- A status hint is shown while active:
  `RESIZE: ←→ adjust · = equalize · Esc done` (arrows match orientation).

## Mouse: drag the divider

The workspace view already runs `MouseModeAllMotion` with click/hover
hit-testing (`view.go`).

- **Hit zone:** the shared border between adjacent panes, ±1 cell for
  forgiveness. Panes render 1-cell borders, so the gutter is a real column
  (or row) on screen. A helper `dividerHitTest(tab, x, y) int` returns the
  divider index (divider *i* sits between pane *i* and *i+1*), or -1.
- **Press** on a divider starts a drag state (`draggingDivider int` on the
  model, -1 when inactive).
- **Motion** while dragging recomputes the two adjacent panes' ratios directly
  from the pointer position (clamped to minimum pane sizes).
- **Release** ends the drag.
- While a drag is active, the existing hover-auto-focus is suppressed so
  panes don't steal focus mid-drag; click-to-focus on a divider press is also
  skipped.

## Error handling / edge cases

- Ratio clamping guarantees no pane below minimum size; if the window itself
  is too small to honour minimums, fall back to the current equal-division
  floor behaviour (`available < n` guard stays).
- Resize mode with a single pane: entering is a no-op (exit immediately or
  don't enter).
- Tab switches or split close while in resize mode / mid-drag: state resets
  cleanly (mode off, drag off).

## Files touched

| File | Change |
|---|---|
| `ui/splitpane.go` | `Ratio` field, proportional `recalcWidths`, `AdjustRatio(idx, deltaCells)`, `Equalize()`, `DividerPositions()` |
| `chord.go` | `ChordResizeMode` action, `s` key |
| `model.go` | resize-mode state + key handling, drag state, reset on tab switch/close |
| `view.go` | `dividerHitTest`, drag press/motion/release wiring, hover-focus suppression, resize-mode status hint |

## Testing

Table-driven tests alongside `ui/splitpane_test.go`:

- Proportional distribution: ratios → expected cell sizes, both orientations,
  remainder goes to last pane.
- Clamping: nudges can't push a pane below minimum; extreme ratios clamp.
- `Equalize()` restores equal sizes.
- Divider hit-testing: positions and ±1 tolerance, both orientations.
- Add/remove split resets ratios to equal.
