// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package challenge

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"errors"
	"fmt"
	"math/big"
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/hypersdk/examples/tokenvm/actions"
)

const (
	bitsPerByte     = 8
	saltLength      = 32
	maxSolutionSize = 128
)

var big1 = big.NewInt(1)

func New() ([]byte, error) {
	b := make([]byte, saltLength)
	_, err := rand.Read(b)
	return b, err
}

func VerifyZKP(senderPublicKey, receiverPublicKey ed25519.PublicKey, assetCommitment, challenge, response *big.Int) error {

	field := big.NewInt(256)

	coefs := make([]*big.Int, 4)
	for i := range coefs {
		coef, err := rand.Int(rand.Reader, field)
		if err != nil {
			fmt.Println(err)
			return err
		}
		coefs[i] = coef
	}

	// Create a new cubic zero-knowledge proof using the field and coefficients
	polynomial := actions.NewPolynomial(coefs...)

	// Create a new cubic zero-knowledge proof
	zkpProof := actions.NewCubicZKProof(field, polynomial)

	// Verify the zero-knowledge proof
	err := zkpProof.VerifyProof(senderPublicKey, receiverPublicKey, assetCommitment, challenge, response)
	if err != nil {
		return errors.New("ZKP verification failed")
	}

	return nil
}

func Verify(salt []byte, solution []byte, difficulty uint16) bool {
	lSalt := len(salt)
	if lSalt != saltLength {
		return false
	}
	lSolution := len(solution)
	if lSolution > maxSolutionSize {
		return false
	}
	// TODO: add more sophisticated algo/make configurable
	h := sha512.New()
	if _, err := h.Write(salt); err != nil {
		return false
	}
	if _, err := h.Write(solution); err != nil {
		return false
	}
	checksum := h.Sum(nil)
	leadingZeros := 0
	for i := 0; i < len(checksum); i++ {
		leading := bits.LeadingZeros8(checksum[i])
		leadingZeros += leading
		if leading < bitsPerByte {
			break
		}
	}
	return leadingZeros >= int(difficulty)
}

func Search(salt []byte, difficulty uint16, cores int) ([]byte, uint64) {
	var (
		solution []byte
		wg       sync.WaitGroup

		attempted uint64
	)
	for i := 0; i < cores; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			var (
				start    = make([]byte, maxSolutionSize/2) // give space to increment without surpassing max solution size
				_, _     = rand.Read(start)
				work     = new(big.Int).SetBytes(start)
				attempts = uint64(0)
			)
			for len(solution) == 0 {
				attempts++

				workBytes := work.Bytes()
				if Verify(salt, workBytes, difficulty) {
					solution = workBytes
					atomic.AddUint64(&attempted, attempts)
					return
				}
				work.Add(work, big1)
			}
			atomic.AddUint64(&attempted, attempts)
		}()
	}
	wg.Wait()
	return solution, attempted
}
