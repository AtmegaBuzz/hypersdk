package actions

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"math/big"
)

type Polynomial struct {
	coefficients []*big.Int
}

// NewPolynomial creates a new cubic polynomial
func NewPolynomial(coefficients ...*big.Int) *Polynomial {
	return &Polynomial{coefficients}
}

// Evaluate evaluates the polynomial at a given x value.
func (p *Polynomial) Evaluate(x *big.Int) *big.Int {
	result := new(big.Int)
	xPower := new(big.Int).Set(x)
	for _, coef := range p.coefficients {
		term := new(big.Int)
		term.Mul(xPower, coef)
		result.Add(result, term)
		xPower.Mul(xPower, x)
	}
	return result
}

// CubicZKProof represents a cubic zero-knowledge proof.
type CubicZKProof struct {
	field      *big.Int
	polynomial *Polynomial
}

// NewCubicZKProof creates a new cubic zero-knowledge proof.
func NewCubicZKProof(field *big.Int, polynomial *Polynomial) *CubicZKProof {
	return &CubicZKProof{
		field:      field,
		polynomial: polynomial,
	}
}

func (p *CubicZKProof) GenerateProof(senderPrivateKey ed25519.PrivateKey, receiverPublicKey ed25519.PublicKey, assetCommitment *big.Int) (*big.Int, *big.Int, error) {
	statement := append(senderPrivateKey.Public().(ed25519.PublicKey), receiverPublicKey...)
	statement = append(statement, assetCommitment.Bytes()...)
	sha256Sum := sha256.Sum256(statement)
	challenge := new(big.Int).SetBytes(sha256Sum[:])

	response := p.polynomial.Evaluate(challenge)

	return challenge, response, nil
}

func (p *CubicZKProof) VerifyProof(senderPublicKey, receiverPublicKey ed25519.PublicKey, assetCommitment, challenge, response *big.Int) error {
	statement := append(senderPublicKey, receiverPublicKey...)
	statement = append(statement, assetCommitment.Bytes()...)
	sha256Sum := sha256.Sum256(statement)
	expectedChallenge := new(big.Int).SetBytes(sha256Sum[:])

	if expectedChallenge.Cmp(challenge) != 0 || p.polynomial.Evaluate(challenge).Cmp(response) != 0 {
		return fmt.Errorf("invalid proof")
	}

	return nil
}
