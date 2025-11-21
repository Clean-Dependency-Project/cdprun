package clamav

import (
	"testing"
)

func TestIsClean(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		want     bool
	}{
		{"clean file", 0, true},
		{"infected file", 1, false},
		{"scan error", 2, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClean(tt.exitCode); got != tt.want {
				t.Errorf("isClean(%d) = %v, want %v", tt.exitCode, got, tt.want)
			}
		})
	}
}

func TestExtractThreats(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "single threat",
			output: "/scan/file: Eicar-Signature FOUND",
			want:   []string{"Eicar-Signature"},
		},
		{
			name: "multiple threats",
			output: `/scan/file1: Win.Trojan.Agent FOUND
/scan/file2: Linux.Malware.Test FOUND`,
			want: []string{"Win.Trojan.Agent", "Linux.Malware.Test"},
		},
		{
			name:   "clean file",
			output: "/scan/file: OK",
			want:   nil,
		},
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractThreats(tt.output)
			if len(got) != len(tt.want) {
				t.Errorf("extractThreats() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractThreats()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractDatabaseDate_FromVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "real version format",
			version: "ClamAV 1.5.1/27805/Mon Oct 27 09:50:30 2025",
			want:    "Mon Oct 27 09:50:30 2025",
		},
		{
			name:    "different date",
			version: "ClamAV 1.0.3/27123/Fri Nov  1 12:34:56 2024",
			want:    "Fri Nov  1 12:34:56 2024",
		},
		{
			name:    "no version",
			version: "unknown",
			want:    "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractDatabaseDate(tt.version); got != tt.want {
				t.Errorf("extractDatabaseDate() = %v, want %v", got, tt.want)
			}
		})
	}
}


func TestParseResult(t *testing.T) {
	tests := []struct {
		name     string
		output   []byte
		exitCode int
		version  string
		wantErr  bool
		validate func(*testing.T, Result)
	}{
		{
			name: "clean file - real output",
			output: []byte(`/scan: OK

----------- SCAN SUMMARY -----------
Known viruses: 8708721
Engine version: 1.5.1
Scanned directories: 0
Scanned files: 1
Infected files: 0
Data scanned: 52 B
Data read: 26 B (ratio 2.00:1)
Time: 8.997 sec (0 m 8 s)
Start Date: 2025:11:01 18:59:37
End Date:   2025:11:01 18:59:46
`),
			exitCode: 0,
			version:  "ClamAV 1.5.1/27805/Mon Oct 27 09:50:30 2025",
			wantErr:  false,
			validate: func(t *testing.T, r Result) {
				if !r.Clean {
					t.Error("expected clean=true")
				}
				if len(r.Threats) != 0 {
					t.Errorf("expected no threats, got %v", r.Threats)
				}
				if r.Metadata.EngineVersion != "ClamAV 1.5.1/27805/Mon Oct 27 09:50:30 2025" {
					t.Errorf("unexpected version: %s", r.Metadata.EngineVersion)
				}
				if r.Metadata.DatabaseDate != "Mon Oct 27 09:50:30 2025" {
					t.Errorf("unexpected date: %s", r.Metadata.DatabaseDate)
				}
			},
		},
		{
			name: "infected file - real output",
			output: []byte(`/scan: Eicar-Signature FOUND

----------- SCAN SUMMARY -----------
Known viruses: 8708721
Engine version: 1.5.1
Scanned directories: 0
Scanned files: 1
Infected files: 1
Data scanned: 69 B
Data read: 69 B (ratio 1.00:1)
Time: 9.002 sec (0 m 9 s)
Start Date: 2025:11:01 18:59:52
End Date:   2025:11:01 19:00:01
`),
			exitCode: 1,
			version:  "ClamAV 1.5.1/27805/Mon Oct 27 09:50:30 2025",
			wantErr:  false,
			validate: func(t *testing.T, r Result) {
				if r.Clean {
					t.Error("expected clean=false")
				}
				if len(r.Threats) != 1 {
					t.Errorf("expected 1 threat, got %d", len(r.Threats))
				}
				if len(r.Threats) > 0 && r.Threats[0] != "Eicar-Signature" {
					t.Errorf("unexpected threat: %s", r.Threats[0])
				}
			},
		},
		{
			name:     "infected but no threats in output",
			output:   []byte("Some output without FOUND"),
			exitCode: 1,
			version:  "ClamAV 1.5.1/27805/Mon Oct 27 09:50:30 2025",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseResult(tt.output, tt.exitCode, tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseResult() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

