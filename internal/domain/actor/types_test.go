package actor

import (
	"gorouter/internal/domain/auth"
	"testing"
)

func TestFrozenValuesAndZeroRejection(t *testing.T) {
	if KindCLI != "cli" || KindJob != "job" || OriginLocal != "local" || OriginRemote != "remote" {
		t.Fatal("actor contract values changed")
	}
	var a auth.Actor
	if IsLocalCLI(a) || IsJob(a) {
		t.Fatal("zero actor must fail closed")
	}
}

func TestCapabilitiesCopy(t *testing.T) {
	in := []Capability{CapabilityStatusRead, CapabilityAuditWrite}
	out := CopyCapabilities(in)
	out[0] = "mutated"
	if in[0] != CapabilityStatusRead {
		t.Fatal("copy aliases input")
	}
	job := CapabilitiesForJob()
	job[0] = "mutated"
	if CapabilitiesForJob()[0] != CapabilityStatusRead {
		t.Fatal("job capabilities must be freshly allocated")
	}
}

func TestClassification(t *testing.T) {
	if !IsLocalCLI(auth.Actor{Kind: KindCLI, Origin: OriginLocal}) {
		t.Fatal("local CLI not classified")
	}
	if IsLocalCLI(auth.Actor{Kind: KindCLI, Origin: OriginRemote}) {
		t.Fatal("remote CLI classified as local")
	}
	if !IsJob(auth.Actor{Kind: KindJob}) {
		t.Fatal("job not classified")
	}
}
