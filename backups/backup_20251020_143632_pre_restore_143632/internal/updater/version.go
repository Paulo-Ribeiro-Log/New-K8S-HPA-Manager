package updater

import (
	"fmt"
	"strconv"
	"strings"
)

// Version é injetada durante o build via -ldflags
// Se não for injetada, usa "dev" como fallback
var Version = "dev"

const (
	RepoOwner = "Paulo-Ribeiro-Log"
	RepoName  = "Scale_HPA"
)

// SemanticVersion representa uma versão semântica
type SemanticVersion struct {
	Major int
	Minor int
	Patch int
}

// ParseVersion converte string "1.2.3" para SemanticVersion
func ParseVersion(v string) (SemanticVersion, error) {
	// Remover prefixo "v" se existir
	v = strings.TrimPrefix(v, "v")

	// Remover sufixo "-dirty" ou "-dev-<hash>" se existir
	if idx := strings.Index(v, "-"); idx != -1 {
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return SemanticVersion{}, fmt.Errorf("versão inválida: %s", v)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return SemanticVersion{}, err
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return SemanticVersion{}, err
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return SemanticVersion{}, err
	}

	return SemanticVersion{Major: major, Minor: minor, Patch: patch}, nil
}

// IsNewerThan verifica se esta versão é mais nova que outra
func (v SemanticVersion) IsNewerThan(other SemanticVersion) bool {
	if v.Major > other.Major {
		return true
	}
	if v.Major == other.Major && v.Minor > other.Minor {
		return true
	}
	if v.Major == other.Major && v.Minor == other.Minor && v.Patch > other.Patch {
		return true
	}
	return false
}

// String retorna string formatada "1.2.3"
func (v SemanticVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
