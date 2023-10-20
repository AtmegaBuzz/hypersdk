package actions

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
)

type Transaction struct {
	From ed25519.PublicKey `json:"from"`

	To ed25519.PublicKey `json:"to"`

	Asset [32]byte `json:"asset"`
}

func (t *Transaction) ExecTx() {

	field := big.NewInt(256)

	coefs := make([]*big.Int, 4)
	for i := range coefs {
		coef, err := rand.Int(rand.Reader, field)
		if err != nil {
			return
		}
		coefs[i] = coef
	}
	polynomial := NewPolynomial(coefs...)

	proof := NewCubicZKProof(field, polynomial)

	challenge, response, err := proof.GenerateProof(t.From, t.To, t.Asset[:])
	if err != nil {
		return
	}

	err = proof.VerifyProof(t.From, t.To, t.Asset[:], challenge, response)
	if err != nil {
		// fmt.Println(err)
		return
	}

}
