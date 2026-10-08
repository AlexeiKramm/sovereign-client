package render

import "testing"

func benchHistory() []PanelHistoryItem {
	long := "The other thing is the background color. In principle, I really do like what you did, but I think it is way too bright. It needs to be significantly more mild. Currently, we think about the Android app."
	return []PanelHistoryItem{
		{ID: "a", TimeAgo: "Just now", CleanText: "Do these comments make sense?", Words: 5, AsrMs: 1081, CleanupMs: 981, ViewMode: "clean"},
		{ID: "b", TimeAgo: "1 min ago", CleanText: long, Words: 114, AsrMs: 1081, CleanupMs: 981, ViewMode: "clean"},
		{ID: "c", TimeAgo: "1 min ago", CleanText: "I like the visual component of that, but the animation may be a bit too long. Right now, it clicks and then takes about half a second.", Words: 44, AsrMs: 4348, CleanupMs: 804, ViewMode: "clean"},
		{ID: "d", TimeAgo: "2 mins ago", CleanText: "Animation plays first and then only after that the thing happens.", Words: 11, AsrMs: 1200, CleanupMs: 559, ViewMode: "clean"},
		{ID: "e", TimeAgo: "26 mins ago", CleanText: "Okay, now it should be working as far as I know.", Words: 11, AsrMs: 833, CleanupMs: 606, ViewMode: "clean"},
	}
}

func BenchmarkRenderPanelFull(b *testing.B) {
	rows := []Row{
		{Kind: RowChips, Text: "Dictation mode", Options: []Option{
			{ID: "mode:auto", Text: "Tap or hold", On: true},
			{ID: "mode:hold", Text: "Hold to talk"},
			{ID: "mode:toggle", Text: "Press to toggle"},
		}},
		{Kind: RowButton, ID: "hotkey:set", Text: "Set custom hotkey…", Detail: "Ctrl+Alt+Space"},
		{Kind: RowChips, Text: "Insertion", Options: []Option{
			{ID: "insertion:paste", Text: "Paste (fast)", On: true},
			{ID: "insertion:type", Text: "Type"},
		}},
		{Kind: RowToggle, ID: "startup", Text: "Start with Windows", On: true},
		{Kind: RowButton, ID: "quit", Text: "Quit Sovereign"},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := RenderPanel(PanelModel{
			Title: "Sovereign", Status: "Ready — Phone", ActiveTab: TabDictations,
			History: benchHistory(), Rows: rows, Version: "v0.9.4", Dark: true, Preset: "cyan", Scale: 1,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderPanelSettings(b *testing.B) {
	rows := []Row{
		{Kind: RowChips, Text: "Dictation mode", Options: []Option{
			{ID: "mode:auto", Text: "Tap or hold", On: true},
			{ID: "mode:hold", Text: "Hold to talk"},
			{ID: "mode:toggle", Text: "Press to toggle"},
		}},
		{Kind: RowButton, ID: "hotkey:set", Text: "Set custom hotkey…", Detail: "Ctrl+Alt+Space"},
		{Kind: RowChips, Text: "Insertion", Options: []Option{
			{ID: "insertion:paste", Text: "Paste (fast)", On: true},
			{ID: "insertion:type", Text: "Type"},
		}},
		{Kind: RowChips, Text: "Overlay position", Options: []Option{
			{ID: "position:bottom-right", Text: "Bottom right", On: true},
			{ID: "position:bottom-left", Text: "Bottom left"},
			{ID: "position:top-right", Text: "Top right"},
			{ID: "position:top-left", Text: "Top left"},
		}},
		{Kind: RowChips, Text: "Theme", Options: []Option{
			{ID: "theme:system", Text: "System", On: true},
			{ID: "theme:dark", Text: "Dark"},
			{ID: "theme:light", Text: "Light"},
		}},
		{Kind: RowDropdown, ID: "dropdown:accent", Text: "Accent color", Detail: "Cyan", On: true, Options: []Option{
			{ID: "preset:cyan", Text: "Cyan", On: true},
			{ID: "preset:sky", Text: "Sky"},
			{ID: "preset:slate", Text: "Slate"},
			{ID: "preset:teal", Text: "Teal"},
			{ID: "preset:amber", Text: "Amber"},
			{ID: "preset:orange", Text: "Orange"},
			{ID: "preset:blue", Text: "Blue"},
			{ID: "preset:green", Text: "Green"},
			{ID: "preset:coral", Text: "Coral"},
			{ID: "preset:lilac", Text: "Lilac"},
			{ID: "preset:mint", Text: "Mint"},
		}},
		{Kind: RowToggle, ID: "startup", Text: "Start with Windows", On: true},
		{Kind: RowToggle, ID: "debug", Text: "Log transcripts (debug)"},
		{Kind: RowButton, ID: "unpair", Text: "Unpair…", Detail: "Phone"},
		{Kind: RowButton, ID: "quit", Text: "Quit Sovereign"},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := RenderPanel(PanelModel{
			Title: "Sovereign", Status: "Ready — Phone", ActiveTab: TabSettings,
			Rows: rows, Version: "v0.9.4", Dark: true, Preset: "cyan", Scale: 1,
		}); err != nil {
			b.Fatal(err)
		}
	}
}
