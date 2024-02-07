package sortition

import (
	"crypto/ed25519"
	"fmt"
	"github.com/yoseplee/vrf"
	"testing"
)

const num uint64 = 50

func TestVRF(t *testing.T) {
	totalIteration := 100
	failCount := 0
	successCount := 0
	for i := 0; i < totalIteration; i++ {
		publicKey, privateKey, _ := ed25519.GenerateKey(nil)
		sortitioner := Sortitioner{threshold: 0.01, pubKey: publicKey, privKey: privateKey}
		ok, proof := sortitioner.Once(num)
		if !ok {
			failCount++
			continue
		}
		res, err := vrf.Verify(publicKey, proof, []byte(fmt.Sprintf("%d", num)))
		if err != nil {
			t.Fatal(err)
		}
		if !res {
			t.Errorf("VRF failed")
		}

		if res {
			successCount++
		}
	}

	fmt.Printf("Count of success: %d, count of failure: %d\n", successCount, failCount)
}
