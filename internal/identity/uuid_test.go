package identity

import (
	"testing"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

func TestUUIDStrategyAndRename(t *testing.T) {
	offline := PlayerUUID("PlayerOne", model.UUIDOffline)
	if offline != OfflineUUID("PlayerOne") || offline[12] != '3' {
		t.Fatal("offline UUID changed")
	}
	random := PlayerUUID("PlayerOne", model.UUIDRandom)
	if len(random) != 32 || random[12] != '4' || random == PlayerUUID("PlayerOne", model.UUIDRandom) {
		t.Fatal("random UUID is not v4")
	}
	if renamedUUID("OtherName", random, model.UUIDRandom) != random {
		t.Fatal("random identity changed on rename")
	}
	if renamedUUID("OtherName", offline, model.UUIDOffline) != OfflineUUID("OtherName") {
		t.Fatal("offline rename did not recalculate identity")
	}
	if renamedUUID("OtherName", offline, model.UUIDPreserved) != offline {
		t.Fatal("imported identity changed on rename")
	}
}
