package playtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boku/internal/platform/sqlite"
)

func TestASessionStartsAtProcessCreationAndEndsWhenItLeaves(t *testing.T) {
	var tracker Tracker
	start := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC)
	seen := start.Add(time.Minute)
	changed, err := tracker.Observe(Sample{
		At:     seen,
		Awake:  time.Minute,
		Biased: time.Minute,
		Games:  []Game{{AppID: "730", Name: "Counter-Strike 2", RunningSince: start}},
	})
	if err != nil || !changed {
		t.Fatalf("observe = %v, %v", changed, err)
	}
	gone := seen.Add(time.Hour)
	if _, err := tracker.Observe(Sample{At: gone, Awake: time.Minute + time.Hour, Biased: time.Minute + time.Hour, Stopped: []Stop{{AppID: "730", At: gone.Add(-time.Second)}}}); err != nil {
		t.Fatal(err)
	}
	sessions := tracker.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d", len(sessions))
	}
	if !sessions[0].StartedAt.Equal(start) || !sessions[0].EndedAt.Equal(gone.Add(-time.Second)) {
		t.Fatalf("session = %s → %s", sessions[0].StartedAt, sessions[0].EndedAt)
	}
}

func TestSleepSplitsASessionAndDoesNotCountTheGap(t *testing.T) {
	var tracker Tracker
	start := time.Date(2026, 10, 29, 3, 0, 0, 0, time.UTC)
	if _, err := tracker.Observe(Sample{
		At: start, Awake: time.Hour, Biased: time.Hour,
		Games: []Game{{AppID: "570", Name: "Dota 2", RunningSince: start}},
	}); err != nil {
		t.Fatal(err)
	}
	woke := start.Add(3 * time.Hour)
	if _, err := tracker.Observe(Sample{
		At: woke, Awake: time.Hour + 10*time.Minute, Biased: time.Hour + 3*time.Hour,
		Games: []Game{{AppID: "570", Name: "Dota 2", RunningSince: start}},
	}); err != nil {
		t.Fatal(err)
	}
	sessions := tracker.Sessions()
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want the run cut across sleep", len(sessions))
	}
	if !sessions[0].EndedAt.Equal(start.Add(10 * time.Minute)) {
		t.Fatalf("first run ended at %s", sessions[0].EndedAt)
	}
	if !sessions[1].StartedAt.Equal(woke) || !sessions[1].EndedAt.IsZero() {
		t.Fatalf("second run = %s → %s", sessions[1].StartedAt, sessions[1].EndedAt)
	}
}

func TestACrashDoesNotFillTheHoleBeforeTheNextLaunch(t *testing.T) {
	start := time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC)
	tracker := Tracker{}
	tracker.Restore([]Session{{
		ID: "1", AppID: "730", Name: "Counter-Strike 2",
		StartedAt: start, LastSeenAt: start.Add(30 * time.Minute),
	}})
	if !tracker.CloseAbandoned() {
		t.Fatal("an open session was left open across a restart")
	}
	back := start.Add(2 * time.Hour)
	if _, err := tracker.Observe(Sample{
		At: back, Awake: time.Hour, Biased: time.Hour,
		Games: []Game{{AppID: "730", Name: "Counter-Strike 2", RunningSince: start}},
	}); err != nil {
		t.Fatal(err)
	}
	sessions := tracker.Sessions()
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d", len(sessions))
	}
	if !sessions[0].EndedAt.Equal(start.Add(30*time.Minute)) || !sessions[1].StartedAt.Equal(back) {
		t.Fatalf("hole was filled: %s→%s and %s→%s", sessions[0].StartedAt, sessions[0].EndedAt, sessions[1].StartedAt, sessions[1].EndedAt)
	}
}

func TestFoldSplitsASessionAcrossAnHourAndAMinute(t *testing.T) {
	start := time.Date(2026, 10, 29, 15, 50, 0, 0, time.UTC)
	sessions := []Session{{
		ID: "1", AppID: "730", Name: "Counter-Strike 2",
		StartedAt: start, EndedAt: start.Add(20 * time.Minute),
	}}
	hours, err := Fold(sessions, start.Truncate(time.Hour), start.Truncate(time.Hour).Add(2*time.Hour), time.Hour, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 2 || hours[0].Seconds != 10*60 || hours[1].Seconds != 10*60 {
		t.Fatalf("hours = %+v", hours)
	}
	minutes, err := Fold(sessions, start, start.Add(2*time.Minute), time.Minute, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(minutes) != 2 || minutes[0].Seconds != 60 || minutes[1].Seconds != 60 {
		t.Fatalf("minutes = %+v", minutes)
	}
	if minutes[0].Name != "Counter-Strike 2" {
		t.Fatalf("name = %q", minutes[0].Name)
	}
}

func TestFoldIncludesAQuietBucketAndAnOpenSession(t *testing.T) {
	start := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	now := start.Add(90 * time.Second)
	points, err := Fold([]Session{{
		ID: "1", AppID: "570", Name: "Dota 2", StartedAt: start.Add(30 * time.Second),
	}}, start, start.Add(3*time.Minute), time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 || points[0].Seconds != 30 || points[1].Seconds != 30 || points[2].Seconds != 0 {
		t.Fatalf("points = %+v", points)
	}
}

func TestThePollIntervalRejectsValuesOutsideItsBounds(t *testing.T) {
	section := NewSection()
	if !section.Current().Enabled || section.Current().Interval() != DefaultPollInterval {
		t.Fatalf("defaults = %+v", section.Current())
	}
	if err := section.Decode(nil); err != nil {
		t.Fatal(err)
	}
	if !section.Current().Enabled || section.Current().Interval() != 2*time.Second {
		t.Fatalf("missing keys changed the defaults: %+v", section.Current())
	}
	encoded, err := section.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if encoded["playtimeEnabled"] != true || encoded["playtimePollIntervalMs"] != int64(2000) {
		t.Fatalf("encoded = %+v", encoded)
	}
	section.Set(Settings{Enabled: false, PollInterval: 500 * time.Millisecond})
	if section.Current().Enabled || section.Current().Interval() != DefaultPollInterval {
		t.Fatalf("too fast = %+v", section.Current())
	}
	section.Set(Settings{Enabled: true, PollInterval: 5 * time.Second})
	if section.Current().Interval() != 5*time.Second {
		t.Fatalf("five seconds became %s", section.Current().Interval())
	}
	section.Set(Settings{Enabled: true, PollInterval: 2 * time.Minute})
	if section.Current().Interval() != DefaultPollInterval {
		t.Fatalf("too slow became %s", section.Current().Interval())
	}
}

func TestLibraryMatchesTheInstallDirectory(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "steamapps", "libraryfolders.vdf"), `
"libraryfolders"
{
	"0" { "path" "`+escape(root)+`" }
	"1" { "path" "`+escape(other)+`" }
}`)
	write(filepath.Join(root, "steamapps", "appmanifest_730.acf"), `
"AppState"
{
	"appid" "730"
	"name" "Counter-Strike 2"
	"installdir" "Counter-Strike"
}`)
	write(filepath.Join(other, "steamapps", "appmanifest_570.acf"), `
"AppState"
{
	"appid" "570"
	"name" "Dota \"2\""
	"installdir" "dota 2"
}`)
	write(filepath.Join(other, "steamapps", "appmanifest_bad.acf"), "not vdf")
	lib, err := LoadLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	id, name, ok := lib.Identify(filepath.Join(other, "steamapps", "common", "dota 2", "game", "bin", "win64", "dota2.exe"))
	if !ok || id != "570" || name != `Dota "2"` {
		t.Fatalf("dota = %s %q %v", id, name, ok)
	}
	id, _, ok = lib.Identify(filepath.Join(root, "steamapps", "common", "Counter-Strike", "cs2.exe"))
	if !ok || id != "730" {
		t.Fatalf("cs = %s %v", id, ok)
	}
	if _, _, ok := lib.Identify(filepath.Join(root, "steam.exe")); ok {
		t.Fatal("steam itself was called a game")
	}
}

func escape(path string) string {
	return filepath.ToSlash(path)
}

func TestSessionsRoundTripAndABadFileIsNotReplaced(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	t.Cleanup(func() { store.Close() })
	start := time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC)
	end := start.Add(4*time.Hour + 23*time.Minute + 7*time.Second)
	if err := store.Save([]Session{{
		ID: "018f", AppID: "730", Name: "Counter-Strike 2",
		StartedAt: start, EndedAt: end, LastSeenAt: end,
	}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Name != "Counter-Strike 2" || !loaded[0].StartedAt.Equal(start) || !loaded[0].EndedAt.Equal(end) {
		t.Fatalf("loaded = %+v", loaded)
	}
	if loaded[0].EndedAt.Sub(loaded[0].StartedAt) != end.Sub(start) {
		t.Fatal("the stored times do not recover the duration")
	}
	store.Close()

	db, err := sqlite.Open(context.Background(), filepath.Join(dir, dbFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Execute(context.Background(), `INSERT INTO schema_migrations (version, applied_at) VALUES (2, 'x')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	broken := NewStore(dir)
	t.Cleanup(func() { broken.Close() })
	if _, err := broken.Load(); err == nil {
		t.Fatal("a future database was accepted")
	}
	if err := broken.Save(nil); err == nil {
		t.Fatal("a database that could not be read was overwritten")
	}
	check, err := sqlite.Open(context.Background(), filepath.Join(dir, dbFile))
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	rows, err := check.Query(context.Background(), `SELECT name FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Values) != 1 || rows.Values[0][0] != "Counter-Strike 2" {
		t.Fatalf("rows = %+v", rows.Values)
	}
}

func TestLegacyJSONIsImportedOnce(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "playtime.json")
	body := []byte(`{"version":1,"sessions":[{"id":"01a18728-e270-7135-84cf-529abdadaf29","appId":"4115450","name":"Phantom Blade Zero","startedAt":"2026-10-29T10:16:06Z","endedAt":"2026-10-29T14:39:13Z","lastSeenAt":"2026-10-29T14:39:13Z"}]}`)
	if err := os.WriteFile(legacy, body, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	t.Cleanup(func() { store.Close() })
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Name != "Phantom Blade Zero" || loaded[0].EndedAt.IsZero() {
		t.Fatalf("imported = %+v", loaded)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("playtime.json was still the live file")
	}
	kept, err := os.ReadFile(legacy + ".imported")
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != string(body) {
		t.Fatal("the imported json was not kept")
	}
	again, err := store.Load()
	if err != nil || len(again) != 1 {
		t.Fatalf("second load = %+v %v", again, err)
	}
}

func TestABrokenLegacyFileIsNotReplacedWhenTheDatabaseIsEmpty(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "playtime.json")
	if err := os.WriteFile(legacy, []byte(`{"version":2,"sessions":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	t.Cleanup(func() { store.Close() })
	if _, err := store.Load(); err == nil {
		t.Fatal("a future json file was accepted")
	}
	if err := store.Save(nil); err == nil {
		t.Fatal("recording continued over a file that could not be imported")
	}
	body, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"version":2,"sessions":[]}` {
		t.Fatalf("json changed to %s", body)
	}
}

func TestSessionsHandsTheUIItsOwnCopy(t *testing.T) {
	recorder := &Recorder{tracker: &Tracker{}}
	recorder.tracker.Restore([]Session{{ID: "1", AppID: "730", Name: "Counter-Strike 2"}})
	got := recorder.Sessions()
	if len(got) != 1 || got[0].Name != "Counter-Strike 2" {
		t.Fatalf("sessions = %+v", got)
	}
	got[0].Name = "changed"
	if recorder.Sessions()[0].Name != "Counter-Strike 2" {
		t.Fatal("the UI copy still points at the recorder")
	}
}
