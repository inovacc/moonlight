package crypto

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"github.com/btcsuite/btcutil/base58"
	"golang.org/x/crypto/argon2"
	"io"
)

var (
	ErrInvalidPayload = errors.New("invalid payload")
)

var instance *crypto

type crypto struct {
	password []byte
}

func NewCrypto(password string) {
	instance = &crypto{
		password: []byte(password),
	}
}

func Encrypt(data []byte) (string, error) {
	return instance.encryptMessage(data)
}

func Decrypt(ciphertext string) ([]byte, error) {
	return instance.decryptMessage(ciphertext)
}

func deriveKey(password, salt []byte, keyLen uint32) []byte {
	return argon2.IDKey(password, salt, 1, 64*1024, 4, keyLen)
}

func VerifyHash(plaintext, ciphertext string) bool {
	return verifyHash(plaintext, ciphertext)

}

func verifyHash(plaintext, ciphertext string) bool {
	hash := sha256.Sum256([]byte(plaintext))
	return bytes.Equal(hash[:], base58.Decode(ciphertext)[len(ciphertext)-32:])
}

func compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func(r *gzip.Reader) {
		_ = r.Close()
	}(r)

	return io.ReadAll(r)
}

func (c *crypto) encryptMessage(plaintext []byte) (string, error) {
	hash := sha256.Sum256(plaintext)

	aesSalt := make([]byte, 16)
	if _, err := rand.Read(aesSalt); err != nil {
		return "", err
	}

	aesKey := deriveKey(c.password, aesSalt, 32)
	aesBlock, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(aesBlock)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	aesCiphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	ciphertext := append(aesSalt, aesCiphertext...)

	compressedData, err := compress(ciphertext)
	if err != nil {
		return "", err
	}

	compressResult := append(compressedData, hash[:]...)

	return base58.Encode(compressResult), nil
}

func (c *crypto) decryptMessage(ciphertext string) ([]byte, error) {
	compressResult := base58.Decode(ciphertext)
	if len(compressResult) < 32 {
		return nil, ErrInvalidPayload
	}

	decompressedResult, err := decompress(compressResult[:len(compressResult)-32])
	if err != nil {
		return nil, err
	}

	aesSalt := decompressedResult[:16]
	aesCiphertext := decompressedResult[16:]

	aesKey := deriveKey(c.password, aesSalt, 32)
	aesBlock, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(aesBlock)
	if err != nil {
		return nil, err
	}

	nonce := aesCiphertext[:gcm.NonceSize()]
	ciphertextOnly := aesCiphertext[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertextOnly, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
