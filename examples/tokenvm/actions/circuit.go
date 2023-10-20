package actions

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
)

// Polynomial represents a polynomial with coefficients.
type Polynomial struct {
	coefficients []*big.Int
}

// NewPolynomial creates a new polynomial with the given coefficients.
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

// GenerateProof generates a zero-knowledge proof of knowledge of the polynomial.
func (p *CubicZKProof) GenerateProof(senderPubKey, receiverPubKey ed25519.PublicKey, hash []byte) (*big.Int, *big.Int, error) {
	// Create a statement that includes sender, receiver, and hash.
	statement := append(senderPubKey, receiverPubKey...)
	statement = append(statement, hash...)

	// Hash the statement to obtain a challenge.
	sha256Sum := sha256.Sum256(statement)

	// Convert the [32]byte array to a []byte slice
	var hashSlice []byte
	copy(hashSlice[:], sha256Sum[:])

	// Create a new big.Int and set its value using SetBytes
	challenge := new(big.Int).SetBytes(hashSlice)

	// Generate a response.
	response := p.polynomial.Evaluate(challenge)

	return challenge, response, nil
}

// VerifyProof verifies a zero-knowledge proof of knowledge of the polynomial.
func (p *CubicZKProof) VerifyProof(senderPubKey, receiverPubKey ed25519.PublicKey, hash []byte, challenge, response *big.Int) error {
	// Recreate the statement for verification.
	statement := append(senderPubKey, receiverPubKey...)
	statement = append(statement, hash...)

	// Hash the statement to obtain the challenge.
	sha256Sum := sha256.Sum256(statement)

	// Convert the [32]byte array to a []byte slice
	var hashSlice []byte
	copy(hashSlice[:], sha256Sum[:])

	// Create a new big.Int and set its value using SetBytes
	expectedChallenge := new(big.Int).SetBytes(hashSlice)

	// Verify the response.
	if expectedChallenge.Cmp(challenge) != 0 || p.polynomial.Evaluate(challenge).Cmp(response) != 0 {
		return fmt.Errorf("invalid proof")
	}

	return nil
}

func main() {
	// Create a new finite field.
	field := big.NewInt(256)

	// Generate a random cubic polynomial.
	coefs := make([]*big.Int, 4)
	for i := range coefs {
		coef, err := rand.Int(rand.Reader, field)
		if err != nil {
			fmt.Println(err)
			return
		}
		coefs[i] = coef
	}

	polynomial := NewPolynomial(coefs...)

	// Generate sender and receiver public keys.
	senderPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Println(err)
		return
	}

	receiverPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Create a hash for the transaction.
	transactionHash := sha256.Sum256([]byte("Transaction data"))

	// Create a new cubic zero-knowledge proof.
	proof := NewCubicZKProof(field, polynomial)

	// Generate and verify a ZK proof for the sender, receiver, and hash.
	challenge, response, err := proof.GenerateProof(senderPubKey, receiverPubKey, transactionHash[:])
	if err != nil {
		fmt.Println(err)
		return
	}

	err = proof.VerifyProof(senderPubKey, receiverPubKey, transactionHash[:], challenge, response)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("The proof is valid for the sender, receiver, and hash.")
}
