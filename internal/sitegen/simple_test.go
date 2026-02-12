package sitegen

import "testing"

func TestArtifactTypeFromFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{name: "tar_xz", filename: "Python-3.14.3.tar.xz", want: "tar.xz"},
		{name: "tar_gz", filename: "python-3.13.11-alpine319-x86_64.tar.gz", want: "tar.gz"},
		{name: "tgz", filename: "foo.tgz", want: "tgz"},
		{name: "rpm", filename: "python-3.13.11-1-1.amzn2023.x86_64.rpm", want: "rpm"},
		{name: "pkg", filename: "python-3.14.3-macos11.pkg", want: "pkg"},
		{name: "exe", filename: "python-3.14.3-amd64.exe", want: "exe"},
		{name: "msi", filename: "node-v22.22.0-x64.msi", want: "msi"},
		{name: "zip", filename: "apache-tomcat-10.1.52.zip", want: "zip"},
		{name: "upper_case", filename: "NODE-V1.2.3-LINUX-X64.TAR.XZ", want: "tar.xz"},
		{name: "no_ext", filename: "README", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := artifactTypeFromFilename(tt.filename); got != tt.want {
				t.Fatalf("artifactTypeFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}
