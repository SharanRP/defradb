// Copyright 2025 Democratized Data Foundation
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package http

import (
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"

	"github.com/sourcenetwork/defradb/crypto"
	"github.com/sourcenetwork/defradb/errors"
)

const (
	// txSignatureHeaderName is the HTTP header containing the transaction operation signature.
	txSignatureHeaderName = "X-DefraDB-Tx-Signature"

	// txSignatureExpiry is the validity duration for transaction operation signatures.
	// Short duration prevents replay attacks.
	txSignatureExpiry = 5 * time.Minute

	// Claim names for the transaction signature JWT
	txActionClaim = "tx_action"
	txIDClaim     = "tx_id"
)

var (
	ErrMissingTxSignature  = errors.New("missing transaction signature")
	ErrInvalidTxSignature  = errors.New("invalid transaction signature")
	ErrExpiredTxSignature  = errors.New("expired transaction signature")
	ErrSignatureMismatch   = errors.New("signature does not match operation")
	ErrAuthRequiredForTxOp = errors.New("authentication required for transaction operations")
)

// SignTxOperation creates a signed JWT payload for a transaction operation.
// The signature proves that the caller has access to the private key for their identity.
//
// Parameters:
//   - privKey: The user's private key for signing
//   - action: The operation being performed ("commit" or "discard")
//   - txID: The transaction identifier
//   - audience: The target server address (for replay protection)
//
// Returns the base64-encoded signed JWT.
func SignTxOperation(privKey crypto.PrivateKey, action string, txID string, audience string) (string, error) {
	now := time.Now()

	token, err := jwt.NewBuilder().
		Claim(txActionClaim, action).
		Claim(txIDClaim, txID).
		Audience([]string{audience}).
		IssuedAt(now).
		Expiration(now.Add(txSignatureExpiry)).
		Build()
	if err != nil {
		return "", err
	}

	// Support the same key types as the auth tokens
	if privKey.Type() != crypto.KeyTypeSecp256k1 && privKey.Type() != crypto.KeyTypeEd25519 {
		return "", crypto.NewErrUnsupportedKeyType(privKey.Type())
	}

	rawKey := privKey.Underlying()
	if secpPrivKey, ok := rawKey.(*secp256k1.PrivateKey); ok {
		rawKey = secpPrivKey.ToECDSA()
	}

	signedToken, err := jwt.Sign(token, jwt.WithKey(keyTypeToJWA(privKey.Type()), rawKey))
	if err != nil {
		return "", err
	}

	return string(signedToken), nil
}

// VerifyTxSignature verifies that a transaction operation signature is valid.
// It checks that:
//   - The signature is valid and was created by the identity's private key
//   - The signature has not expired
//   - The action and txID match the expected values
//   - The audience matches this server
//
// Parameters:
//   - pubKey: The user's public key for verification
//   - signature: The base64-encoded signed JWT from the request header
//   - expectedAction: The operation being performed ("commit" or "discard")
//   - expectedTxID: The transaction identifier from the URL
//   - expectedAudience: This server's address
func VerifyTxSignature(
	pubKey crypto.PublicKey,
	signature string,
	expectedAction string,
	expectedTxID string,
	expectedAudience string,
) error {
	if signature == "" {
		return ErrMissingTxSignature
	}

	// Support the same key types as the auth tokens
	if pubKey.Type() != crypto.KeyTypeSecp256k1 && pubKey.Type() != crypto.KeyTypeEd25519 {
		return crypto.NewErrUnsupportedKeyType(pubKey.Type())
	}

	rawKey := pubKey.Underlying()
	if secpPubKey, ok := rawKey.(*secp256k1.PublicKey); ok {
		rawKey = secpPubKey.ToECDSA()
	}

	// Verify the signature
	_, err := jws.Verify([]byte(signature), jws.WithKey(keyTypeToJWA(pubKey.Type()), rawKey))
	if err != nil {
		return ErrInvalidTxSignature
	}

	// Parse and validate the token claims
	token, err := jwt.Parse([]byte(signature), jwt.WithVerify(false), jwt.WithAudience(expectedAudience))
	if err != nil {
		return ErrInvalidTxSignature
	}

	// Check expiration
	if token.Expiration().Before(time.Now()) {
		return ErrExpiredTxSignature
	}

	// Verify the action matches
	action, ok := token.Get(txActionClaim)
	if !ok || action != expectedAction {
		return ErrSignatureMismatch
	}

	// Verify the transaction ID matches
	txID, ok := token.Get(txIDClaim)
	if !ok || txID != expectedTxID {
		return ErrSignatureMismatch
	}

	return nil
}

// keyTypeToJWA maps a crypto.KeyType to the corresponding JWA signature algorithm.
func keyTypeToJWA(keyType crypto.KeyType) jwa.SignatureAlgorithm {
	if keyType == crypto.KeyTypeEd25519 {
		return jwa.EdDSA
	}
	return jwa.ES256K
}
