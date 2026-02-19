package hfpoc

// AssessRules is a YAML-friendly wrapper for intake criteria plus required files.
// We keep this separate from Policy because the PoC currently uses different
// schemas for assess vs clone/verify.
type AssessRules struct {
	Criteria      IntakeCriteria `yaml:"criteria" json:"criteria"`
	RequiredFiles []string       `yaml:"required_files" json:"required_files"`
}
