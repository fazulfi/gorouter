package media

import (
	"testing"
)

// TestAllModalitiesExhaustive pins the modality set to the audited upstream
// route inventory (audit/01-http-contracts.md:544-557) plus the imageToText
// service kind (audit/02-provider-matrix.md:58). Removing or renaming a
// modality requires an upstream citation change.
func TestAllModalitiesExhaustive(t *testing.T) {
	want := []Modality{
		ModalityEmbeddings,
		ModalityImageGeneration,
		ModalityImageToText,
		ModalityTTS,
		ModalitySTT,
		ModalityVoices,
		ModalityWebSearch,
		ModalityWebFetch,
		ModalityVideoGeneration,
		ModalityVideoEdit,
		ModalityVideoExtension,
		ModalityVideoStatus,
	}
	got := AllModalities()
	if len(got) != len(want) {
		t.Fatalf("AllModalities() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllModalities()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAllModalitiesDoesNotAliasBackingSlice(t *testing.T) {
	a := AllModalities()
	b := AllModalities()
	a[0] = "mutated"
	if b[0] != ModalityEmbeddings {
		t.Fatalf("AllModalities returned an aliasing slice")
	}
}

func TestValid(t *testing.T) {
	for _, m := range AllModalities() {
		if !Valid(m) {
			t.Errorf("Valid(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"", "chat", "embedding", "IMAGE_GENERATION", "video ", "unknown"} {
		if Valid(Modality(m)) {
			t.Errorf("Valid(%q) = true, want false (unknown modalities fail closed)", m)
		}
	}
}

func TestParseModality(t *testing.T) {
	got, ok := ParseModality("  TTS ")
	if !ok || got != ModalityTTS {
		t.Errorf("ParseModality(TTS) = %q,%v want tts,true", got, ok)
	}
	if _, ok := ParseModality("chat"); ok {
		t.Errorf("ParseModality(chat) ok = true, want false")
	}
}
