package vfs

import (
	"testing"

	"pigcloud/internal/filetypes"
)

func TestValidateFile_ExtensionsTrackRegistry(t *testing.T) {
	cases := []string{
		"pdf", "jpg", "png", "gif", "svg", "webp",
		"mp3", "wav", "flac", "mp4", "mov", "webm",
		"zip", "tar", "gz", "7z", "rar",
		"py", "go", "rs", "js", "mjs", "ts", "html", "css", "json", "yaml", "toml", "md",
		"sh", "ps1", "bat", "Dockerfile", "Makefile",
		"exe", "deb", "rpm", "msi", "dmg", "apk",
		"scr", "fakeext123", "qqqqqqq", "definitely-not-a-real-ext",
	}

	for _, ext := range cases {
		t.Run(ext, func(t *testing.T) {
			name := "test." + ext
			ok, reason := ValidateFile(name, 1024)
			registryType := filetypes.TypeOf(ext)
			wantOK := registryType != "other"
			if ok != wantOK {
				t.Errorf("ValidateFile(%q) ok=%v but filetypes.TypeOf(%q)=%q (want ok=%v); reason=%q",
					name, ok, ext, registryType, wantOK, reason)
			}
		})
	}
}

func TestValidateFile_SizeLimit(t *testing.T) {
	if ok, _ := ValidateFile("big.pdf", MaxUploadSize+1); ok {
		t.Errorf("file above MaxUploadSize must be rejected")
	}
	if ok, _ := ValidateFile("zero.png", 0); !ok {
		t.Errorf("zero-byte file must be accepted")
	}
}

func TestValidateFile_NameCharacters(t *testing.T) {
	if ok, _ := ValidateFile("bad name!.txt", 1024); ok {
		t.Errorf("name with !-character must be rejected (matches REGEX_FILE_NAME)")
	}
	if ok, reason := ValidateFile("Geschäftsbericht.pdf", 1024); !ok {
		t.Errorf("umlaut name must be accepted: %s", reason)
	}
}

func TestValidateFile_ExtensionlessAllowed(t *testing.T) {
	for _, name := range []string{"no-ext-file", "Makefile", "LICENSE", "README"} {
		t.Run(name, func(t *testing.T) {
			if ok, _ := ValidateFile(name, 1024); !ok {
				t.Errorf("extensionless file %q must be accepted", name)
			}
		})
	}
}

func TestValidateDirName(t *testing.T) {
	tests := []struct {
		name   string
		wantOK bool
	}{
		{"Documents", true},
		{"My Photos", true},
		{"test-dir", true},
		{"test_dir", true},
		{"Geschäftsberichte", true},
		{"bad/name", false},
		{"bad.name", false},
		{"bad@name", false},
	}
	for _, tc := range tests {
		ok, _ := ValidateDirName(tc.name)
		if ok != tc.wantOK {
			t.Errorf("ValidateDirName(%q) = %v, want %v", tc.name, ok, tc.wantOK)
		}
	}
}

func TestIsSafeName_Escapes(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "../etc", "a/b", "a\\b", "x\x00y"} {
		if IsSafeName(bad) {
			t.Errorf("IsSafeName(%q) = true, want false (path escape)", bad)
		}
	}
	for _, good := range []string{"report.txt", "Fotos", "my_file-1.pdf"} {
		if !IsSafeName(good) {
			t.Errorf("IsSafeName(%q) = false, want true", good)
		}
	}
}

func TestIsExcludedDir(t *testing.T) {
	if !IsExcludedDir(".Trash") {
		t.Error(".Trash should be excluded")
	}
	if !IsExcludedDir(".Favorites") {
		t.Error(".Favorites should be excluded")
	}
	if !IsExcludedDir(".Recents") {
		t.Error(".Recents should be excluded")
	}
	if IsExcludedDir("Documents") {
		t.Error("Documents should not be excluded")
	}
}
