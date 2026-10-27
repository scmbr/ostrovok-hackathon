package registry

import (
	"testing"

	"github.com/ostrovok-hackathon-2026/tima-mechty/backend/internal/domain"
	embedded "github.com/ostrovok-hackathon-2026/tima-mechty/backend/registry"
)

// Во встроенном реестре нет пересечений синонимов: общие фразы объявлены только через ambiguous_synonyms.
func TestEmbeddedRegistryHasNoSynonymOverlaps(t *testing.T) {
	reg, err := Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range domain.SynonymOverlaps(reg) {
		t.Errorf("%s «%s» пересекается с %s «%s»", o.KeyA, o.PhraseA, o.KeyB, o.PhraseB)
	}
}
