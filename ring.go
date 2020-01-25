// Copyright (c) 2020 Prodefi -  D FROZEN SOFT PRIVATE LIMITED

package main

import (
	"crypto/sha256"
	"encoding/json"
	"math/big"
)

// A Ring is a number of public/private key pairs
type Ring struct {
	PubKeys  []CurvePoint `json:"pubkeys"`
	PrivKeys []*big.Int   `json:"privkeys"`
}

// MarshalJSON converts a Ring to a JSON representation
func (r *Ring) MarshalJSON() ([]byte, error) {
	pks := make([]*hexBig, len(r.PrivKeys))
	for i, v := range r.PrivKeys {
		pks[i] = (*hexBig)(v)
	}

	return json.Marshal(&struct {
		PubKeys  []CurvePoint `json:"pubkeys"`
		PrivKeys []*hexBig    `json:"privkeys"`
	}{
		PubKeys:  r.PubKeys,
		PrivKeys: pks,
	})
}

// UnmarshalJSON converts a JSON representation to a Ring struct
func (r *Ring) UnmarshalJSON(data []byte) error {
	var aux struct {
		PubKeys  []CurvePoint `json:"pubkeys"`
		PrivKeys []*hexBig    `json:"privkeys"`
	}
	err := json.Unmarshal(data, &aux)
	if err != nil {
		return err
	}

	pks := make([]*big.Int, len(aux.PrivKeys))
	for i, v := range aux.PrivKeys {
		pks[i] = (*big.Int)(v)
	}
	r.PrivKeys = pks[:]
	r.PubKeys = aux.PubKeys
	return nil
}

func convert(data []byte) *big.Int {
	z := new(big.Int)
	z.SetBytes(data)
	return z
}

var curveB = new(big.Int).SetInt64(3)

// PublicKeysHashed returns the hashed public key for the given Ring
func (r Ring) PublicKeysHashed() [sha256.Size]byte {
	var out [sha256.Size]byte

	for i := 0; i < len(r.PubKeys); i++ {
		out = sha256.Sum256(append(out[:], r.PubKeys[i].Marshal()...))
	}

	return out
}

// Generate creates public and private keypairs for a ring with the size of n
func (r *Ring) Generate(n int) error {
	for i := 0; i < n; i++ {
		public, private, err := generateKeyPair()
		if err != nil {
			return err
		}
		r.PrivKeys = append(r.PrivKeys, private)
		r.PubKeys = append(r.PubKeys, *public)
	}

	return nil
}

// PubKeyIndex returns the index of a public key
func (r *Ring) PubKeyIndex(pk CurvePoint) int {

	for i, pub := range r.PubKeys {
		if pub == pk {
			return i
		}
	}

	return -1

}

// Signature generates a signature
func (r *Ring) Signature(pk *big.Int, message []byte, signer int) (*RingSignature, error) {
	N := CurvePoint{}.Order()

	// Message is a 256 bit token which uniquely identifies the Ring and the public keys
	// of all of its participants
	var messageHash [32]byte
	copy(messageHash[:], message)
	hashp := NewCurvePointFromHash(messageHash)

	// Calculate Tau
	pk.Mod(pk, N)
	hashSP := hashp.ScalarMult(pk)

	// hashout = H(hash.X, tau)
	hashAcc := sha256.Sum256(append(hashp.Marshal()[:32], hashSP.Marshal()...))

	n := len(r.PubKeys)
	var ctlist []*big.Int //This has to be 2n so here we have n = 4 so 2n = 8 :)
	var a, b CurvePoint
	var ri *big.Int

	csum := big.NewInt(0)

	for j := 0; j < n; j++ {

		if j != signer {
			cj := CurvePoint{}.RandomN()
			tj := CurvePoint{}.RandomN()

			a = r.PubKeys[j].ParameterPointAdd(tj, cj)

			b = hashp.HashPointAdd(hashSP, tj, cj)
			ctlist = append(ctlist, cj)
			ctlist = append(ctlist, tj)
			csum.Add(csum, cj)
		}

		if j == signer {
			dummy := big.NewInt(0)
			ctlist = append(ctlist, dummy)
			ctlist = append(ctlist, dummy)
			ri = CurvePoint{}.RandomN()
			a = CurvePoint{}.ScalarBaseMult(ri)
			b = hashp.ScalarMult(ri)
		}

		hashAcc = sha256.Sum256(append(hashAcc[:], append(a.Marshal(), b.Marshal()...)...))
	}

	hashb := new(big.Int).SetBytes(hashAcc[:])
	hashb.Mod(hashb, N)

	csum.Mod(csum, N)
	c := new(big.Int).Sub(hashb, csum)
	c.Mod(c, N)

	cx := new(big.Int).Mul(c, pk)
	cx.Mod(cx, N)
	ti := new(big.Int).Sub(ri, cx)
	ti.Mod(ti, N)
	ctlist[2*signer] = c
	ctlist[2*signer+1] = ti

	return &RingSignature{hashSP, ctlist}, nil
}

// Signatures generates a signature given a message
func (r *Ring) Signatures(message []byte) ([]RingSignature, error) {

	var signaturesArr []RingSignature

	for i, privKey := range r.PrivKeys {
		pub := r.PubKeys[i]
		signerNumber := r.PubKeyIndex(pub)
		signature, err := r.Signature(privKey, message, signerNumber)
		if err != nil {
			return nil, err
		}
		signaturesArr = append(signaturesArr, *signature)
	}

	return signaturesArr, nil
}

// VerifySignature verifys a signature given a message
func (r *Ring) VerifySignature(message []byte, sigma RingSignature) bool {
	// ring verification
	// assumes R = pk1, pk2, ..., pkn
	// sigma = H(m||R)^x_i, c1, t1, ..., cn, tn = taux, tauy, c1, t1, ..., cn, tn
	tau := sigma.Tau
	ctlist := sigma.Ctlist
	n := len(r.PubKeys)
	N := CurvePoint{}.Order() //group.N

	var messageHash [32]byte
	copy(messageHash[:], message)
	hashp := NewCurvePointFromHash(messageHash)

	hashAcc := sha256.Sum256(append(hashp.Marshal()[:32], tau.Marshal()...))

	csum := big.NewInt(0)

	for j := 0; j < n; j++ {
		cj := ctlist[2*j]
		tj := ctlist[2*j+1]
		cj.Mod(cj, N)
		tj.Mod(tj, N)

		yc := r.PubKeys[j].ScalarMult(cj)     // y^c = g^(xc)
		gt := CurvePoint{}.ScalarBaseMult(tj) // g^t + y^c
		gt = gt.Add(yc)

		tauc := tau.ScalarMult(cj) //H(m||R)^(xc)
		H := hashp.ScalarMult(tj)  //H(m||R)^t
		H = H.Add(tauc)            // fieldJacobianToBigAffine `normalizes' values before returning so yes - normalize uses fast reduction using specialised form of secp256k1's prime! :D

		hashAcc = sha256.Sum256(append(hashAcc[:], append(gt.Marshal(), H.Marshal()...)...))

		csum.Add(csum, cj)
		csum.Mod(csum, N)
	}

	hashout := new(big.Int).SetBytes(hashAcc[:])
	hashout.Mod(hashout, N)
	csum.Mod(csum, N)
	return csum.Cmp(hashout) == 0
}
