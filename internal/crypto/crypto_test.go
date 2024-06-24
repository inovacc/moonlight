package crypto

import (
	"testing"
)

func TestCrypto(t *testing.T) {
	NewCrypto("password")

	lorem := "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Egestas tellus rutrum tellus pellentesque eu tincidunt tortor aliquam nulla. Quisque egestas diam in arcu cursus euismod quis viverra. Vestibulum morbi blandit cursus risus. Et sollicitudin ac orci phasellus egestas tellus rutrum tellus. Convallis posuere morbi leo urna molestie. At elementum eu facilisis sed odio. Pretium fusce id velit ut tortor pretium viverra. Nunc mi ipsum faucibus vitae aliquet nec. Aliquam nulla facilisi cras fermentum odio. Nunc mattis enim ut tellus elementum sagittis. Dui accumsan sit amet nulla facilisi morbi tempus iaculis urna. Nunc mattis enim ut tellus elementum. Eget felis eget nunc lobortis mattis aliquam faucibus."

	// Encrypt
	data, err := Encrypt([]byte(lorem))
	if err != nil {
		t.Error(err)
	}

	if data == "" {
		t.Error("data is empty")
	}

	// Decrypt
	decrypted, err := Decrypt(data)
	if err != nil {
		t.Error(err)
	}

	if string(decrypted) != lorem {
		t.Error("decrypted data not match")
	}
}
