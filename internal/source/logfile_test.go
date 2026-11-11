package source

import "testing"

func TestIsLogFile(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		files map[string]bool // paths that exist
		want  bool
	}{
		{"log extension", "/var/log/app.log", nil, true},
		{"underscore separator", "/srv/app/access_log", nil, true},
		{"plural with dash", "/srv/app/error-logs.txt", nil, true},
		{"rotated", "/srv/app/app.log.1", nil, true},
		{"log folder", "/var/log/syslog", nil, true},
		{"pm2 logs folder", "~/.pm2/logs/api-out.txt", nil, true},
		{"upper case", "/srv/app/APP.LOG", nil, true},
		{"log inside a longer word", "/srv/shop/catalog.json", nil, false},
		{"blog", "/srv/blog/blog.sqlite", nil, false},
		{"dialog", "/srv/app/dialog.db", nil, false},
		{"no log at all", "/data/app.db", nil, false},
		{"folder with log inside a word", "/srv/catalog/items.json", nil, false},
		{
			"turbopack LOG with CURRENT",
			"/app/.next/dev/cache/turbopack/f37fad94/LOG",
			map[string]bool{"/app/.next/dev/cache/turbopack/f37fad94/CURRENT": true},
			false,
		},
		{
			"LOG.old with CURRENT",
			"/db/LOG.old",
			map[string]bool{"/db/CURRENT": true},
			false,
		},
		{
			"LOG.old.digits with CURRENT",
			"/db/LOG.old.1700000000",
			map[string]bool{"/db/CURRENT": true},
			false,
		},
		{"LOG without CURRENT", "/srv/app/LOG", nil, true},
		{
			"LOG.old.text with CURRENT is not a database log name",
			"/db/LOG.old.bak",
			map[string]bool{"/db/CURRENT": true},
			true,
		},
		{
			"lower case log with CURRENT is not a database log name",
			"/db/log",
			map[string]bool{"/db/CURRENT": true},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists := func(p string) bool { return tt.files[p] }
			if got := IsLogFile(tt.path, exists); got != tt.want {
				t.Errorf("IsLogFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsLogFileNilExists(t *testing.T) {
	if !IsLogFile("/srv/app/LOG", nil) {
		t.Error("a nil exists function should treat CURRENT as missing")
	}
}

func TestIsLogFileChecksCurrentOnlyForDatabaseNames(t *testing.T) {
	calls := 0
	exists := func(string) bool { calls++; return false }
	IsLogFile("/var/log/app.log", exists)
	IsLogFile("/srv/shop/catalog.json", exists)
	if calls != 0 {
		t.Errorf("exists was called %d times for names that are not database logs", calls)
	}
}
