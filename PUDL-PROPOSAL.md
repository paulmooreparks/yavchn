# PUDL responsive workspace proposal

This proposal records the issues found while adapting YAVCHN to phone, tablet, and desktop widths. It is ready for the PUDL agent as of 3 October 2026. The observations apply to YAVCHN's bundled PUDL 0.40.0. Please check the current PUDL source before implementing, since a newer release may already address some items.

YAVCHN has implemented its reader layout and site controls. The remaining work concerns shared window capabilities, menu overflow, and reusable control behavior. No new PUDL vendor patches were introduced for this review. Please implement shared fixes in PUDL source, document the public contract, and release them for downstream adoption.

## 1. Restrict window placement in narrow workspaces

This is the highest-priority unresolved issue. On a phone, YAVCHN's readers visually fill the available workspace, but PUDL still treats them as floating windows. Their menus offer Restore, snapping, docking, and reset even though those placements are not useful at that width. CSS alone cannot keep menu capabilities, keyboard operations, and window state consistent.

Provide a documented host opt-in for a responsive placement policy. YAVCHN needs a maximized-only workspace below its 640px layout breakpoint, with minimization still supported. Base the decision on available workspace width rather than user-agent detection. The policy must constrain actual window behavior, not merely filter a menu.

Required behavior:

- Visible readers occupy the available workspace as maximized windows, without floating frames, resize handles, or draggable placement.
- Minimize remains enabled. Selecting a minimized reader restores its visibility in maximized form.
- Restore to floating is visible but disabled while the restriction applies. This is distinct from restoring a minimized reader.
- Snap, dock, undock, movement, resizing, and placement reset are unavailable while restricted. Content commands, sharing, open-as-page, and close continue to work.
- Window chrome, the window menu, generated Window menu, pointer gestures, title-bar double-click, keyboard commands, and script APIs enforce the same policy. Previously captured command callbacks cannot bypass it.
- Opening a saved URL with a floating or docked placement in a narrow workspace yields a valid maximized presentation. Widening recovers the prior wide-screen placement and dock assignment without losing the article, scroll positions, or minimized state.
- Changing width while a menu or gesture is active leaves a valid state. The policy must cover newly opened and restored windows as well as existing ones.

PUDL 0.40.0's documented `menuCommands()` returns command snapshots; changing them does not change capabilities. The `pudl:window-menu` event permits additions, not restrictions. Please design an explicit public policy rather than requiring host interception of generated menu elements. Document how constrained presentation interacts with URL state, history, and stored placement.

Verify floating, docked, maximized, minimized, duplicate, and content-sized windows across the breakpoint. YAVCHN can opt its readers into the policy; other applets must retain their declared sizing rules.

## 2. Make mobile menu overflow discoverable

In the mobile Window menu, the Open windows heading appeared at the bottom while both window entries were below the clipped edge. The auto-hiding scrollbar provided no persistent indication that the menu could scroll. The current menu panel height limit includes a 32rem cap.

On narrow screens, use the available viewport height after accounting for the menu anchor and safe areas. When content still overflows, show persistent directional overflow cues. The cues should indicate independently whether more content exists above or below, update during scrolling and resizing, and disappear when the corresponding edge is reached. They must not cover or intercept menu commands.

Verify a long generated Window menu, nested menus and Back navigation, keyboard focus scrolling, touch scrolling, landscape orientation, browser zoom, and viewport changes caused by browser chrome or the on-screen keyboard. Opening a submenu must leave its first actionable item discoverable. Do not rely on platform scrollbar visibility.

## 3. Improve splitter targets and document single-pane presentation

The original reader splitter was difficult to acquire by finger or mouse. YAVCHN now uses an 18px divider in both desktop and mobile layouts. It also offers Article, Discussion, and Split modes so phone users can read without manipulating a divider.

Provide a documented splitter target-size option or token that can enlarge the interactive region without overlapping adjacent links or stealing normal content scrolling. Retain keyboard resizing, orientation semantics, limits, and visible focus. An 18px divider is YAVCHN's accepted starting point; it need not become a mandatory global size.

Document or add a supported single-pane mode that hides the other pane and divider while retaining both mounted pane contents and the split proportion. YAVCHN currently owns its pane state, toggles the split layout, and refreshes PUDL when returning to Split. Confirm that supported pattern or provide a direct API. Per-reader choices, article labels, scroll restoration, and sharing remain host responsibilities.

Verify horizontal and vertical splits, pointer cancellation, touch scrolling near the divider, minimum pane sizes, keyboard resizing, and repeated transitions between single and split panes.

## 4. Support compact window chrome and readable glyphs

YAVCHN hides secondary title-bar buttons at narrow widths and exposes their actions through the window menu. The menu button remains 40px square, but its original 12px caret was too small within that button. YAVCHN now uses a 20px outlined downward chevron for narrow readers.

Consider a documented compact-chrome option and separate glyph-size and target-size tokens. The compact title bar must retain its accessible title, active-window indication, and access to every applicable command. PUDL should suppress gesture instructions that are invalid under the restricted placement policy. Verify dark and light themes, forced colors, enlarged text, and keyboard-only access.

## 5. Document workspace-level taskbars and pane navigation

YAVCHN previously placed its taskbar beneath the detail pane. The mobile story-list view therefore hid every open-window entry. The taskbar now sits outside the sidebar/detail switch, spans the whole workspace, and reserves space below both panes. Overflow arrows keep additional window entries reachable when scrollbars disappear.

Please document the supported placement of `data-win-dock` outside the detail pane. If any runtime assumptions prevent this arrangement, remove those assumptions through a public contract. YAVCHN's current placement works and does not require a new API merely for its own sake.

YAVCHN also provides a taskbar sidebar button. On wide layouts it toggles the list; on phones it switches between stories and windows using public minimize/raise operations, preserving the previously visible window set. This replaces the title-bar back link. Consider whether master-detail should expose a pane-navigation operation independent of window minimization. If added, define its interaction with window activation, URL navigation, reload, and focus so hosts do not compete with automatic pane selection.

## 6. Offer consistent menu-bar elevation through theme tokens

Theme Studio previews use standard PUDL selectors, but YAVCHN's earlier component overrides obscured the palette's effect. Those selector overrides have been removed. In light mode the top-bar surface was also too close to the selector track, making identical selectors appear different depending on their surroundings.

The accepted YAVCHN presentation keeps selectors recessed and gives menu-bar groups the same track background with raised elevation. The local menu-bar rule uses `--recess-bg` for the background and PUDL's `--raise-border` and `--raise-shadow`; a one-pixel border replaces one pixel of padding so dimensions stay constant. Menu items, open states, and dropdown panels retain their existing behavior and styling. The light-mode top-bar background uses `--surface`; dark mode retains its existing background.

Consider explicit menu-bar surface and elevation tokens, or document this composition as supported. Preserve elevation as the clickability cue in both themes. Do not convert this proposal into a redesign of menu items or dropdowns; those remain under separate review.

## Scope and handoff

The host retains its feed choices, reader pane labels, toolbar composition, sidebar pin/open/hide placement, active-reader reuse, and palette values. YAVCHN's source and acceptance notes are in `DESIGN.md`; relevant implementations are in `src/static/style.css`, `desktop.js`, `story.js`, and `readers.js`, with templates under `src/templates`. Its browser regression suite is `tests/readers.browser.cjs`.

Please prioritize the restricted placement policy and menu overflow fixes. Return the documented API or markup contract, regression coverage, release version, and adoption instructions. Distinguish implemented fixes from optional design recommendations so YAVCHN can adopt the release without depending on undocumented behavior.
