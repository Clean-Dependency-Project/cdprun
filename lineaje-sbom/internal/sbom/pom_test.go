package sbom

import (
	"os"
	"path/filepath"
	"testing"
)

const minimalPOM = `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>minimal</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>6.2.3</version>
    </dependency>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-web</artifactId>
      <version>6.2.3</version>
    </dependency>
  </dependencies>
</project>
`

func TestPURLsFromPOM_Minimal(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "pom.xml")
	if err := os.WriteFile(f, []byte(minimalPOM), 0644); err != nil {
		t.Fatal(err)
	}
	purls, err := PURLsFromPOM(f)
	if err != nil {
		t.Fatalf("PURLsFromPOM: %v", err)
	}
	if len(purls) != 2 {
		t.Fatalf("len(purls) = %d, want 2", len(purls))
	}
	want := map[string]bool{
		"pkg:maven/org.springframework/spring-core@6.2.3": true,
		"pkg:maven/org.springframework/spring-web@6.2.3":  true,
	}
	for _, p := range purls {
		if !want[p] {
			t.Errorf("unexpected purl %q", p)
		}
	}
}

func TestPURLsFromPOM_NoFile(t *testing.T) {
	_, err := PURLsFromPOM(filepath.Join(t.TempDir(), "nonexistent.xml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPURLsFromPOM_InvalidXML(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.xml")
	if err := os.WriteFile(f, []byte(`<project><unclosed`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := PURLsFromPOM(f)
	if err == nil {
		t.Fatal("expected error for invalid XML")
	}
}

func TestPURLsFromPOM_EmptyDependencies(t *testing.T) {
	emptyPOM := `<?xml version="1.0"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>empty</artifactId>
  <version>1.0</version>
</project>
`
	dir := t.TempDir()
	f := filepath.Join(dir, "pom.xml")
	if err := os.WriteFile(f, []byte(emptyPOM), 0644); err != nil {
		t.Fatal(err)
	}
	purls, err := PURLsFromPOM(f)
	if err != nil {
		t.Fatalf("PURLsFromPOM: %v", err)
	}
	if len(purls) != 0 {
		t.Errorf("len(purls) = %d, want 0", len(purls))
	}
}
