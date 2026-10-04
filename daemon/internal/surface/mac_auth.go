package surface

import (
	"crypto/aes"
	"crypto/md5" // Apple Remote Desktop's existing wire protocol requires MD5.
	"crypto/rand"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"strings"

	vnc "github.com/kward/go-vnc"
)

// Apple Remote Desktop authentication stays on the Mac. Phones authenticate with
// their existing SSH key and never receive the Mac's username/password.
type appleDesktopAuth struct {
	conn     net.Conn
	username string
	password string
}

func (*appleDesktopAuth) SecurityType() uint8 { return 30 }

func (a *appleDesktopAuth) Handshake(_ *vnc.ClientConn) error {
	if len(a.username) == 0 || len(a.username) > 63 || len(a.password) == 0 || len(a.password) > 63 ||
		strings.ContainsRune(a.username, 0) || strings.ContainsRune(a.password, 0) {
		return problem("desktop_credentials", "Mac screen sharing requires a username and password of at most 63 UTF-8 bytes.")
	}
	var header [4]byte
	if _, err := io.ReadFull(a.conn, header[:]); err != nil {
		return err
	}
	generator := binary.BigEndian.Uint16(header[:2])
	size := int(binary.BigEndian.Uint16(header[2:]))
	if generator < 2 || size < 64 || size > 512 {
		return problem("desktop_auth_protocol", "The Mac returned unsupported screen-sharing authentication.")
	}
	parameters := make([]byte, size*2)
	if _, err := io.ReadFull(a.conn, parameters); err != nil {
		return err
	}
	prime := new(big.Int).SetBytes(parameters[:size])
	peer := new(big.Int).SetBytes(parameters[size:])
	one := big.NewInt(1)
	upper := new(big.Int).Sub(prime, one)
	if prime.BitLen() < 512 || !prime.ProbablyPrime(20) || peer.Cmp(one) <= 0 || peer.Cmp(upper) >= 0 ||
		new(big.Int).SetUint64(uint64(generator)).Cmp(upper) >= 0 {
		return problem("desktop_auth_protocol", "The Mac returned invalid screen-sharing authentication.")
	}
	private, err := rand.Int(rand.Reader, new(big.Int).Sub(prime, big.NewInt(3)))
	if err != nil {
		return err
	}
	private.Add(private, big.NewInt(2))
	public := new(big.Int).Exp(new(big.Int).SetUint64(uint64(generator)), private, prime)
	shared := new(big.Int).Exp(peer, private, prime).FillBytes(make([]byte, size))
	key := md5.Sum(shared)
	clear(shared)
	block, err := aes.NewCipher(key[:])
	clear(key[:])
	if err != nil {
		return err
	}
	credentials := make([]byte, 128)
	if _, err := rand.Read(credentials); err != nil {
		return err
	}
	defer clear(credentials)
	copy(credentials[:64], a.username)
	credentials[len(a.username)] = 0
	copy(credentials[64:], a.password)
	credentials[64+len(a.password)] = 0
	// ARD specifies independent AES blocks, followed by the DH public key.
	response := make([]byte, 128+size)
	for offset := 0; offset < len(credentials); offset += block.BlockSize() {
		block.Encrypt(response[offset:offset+block.BlockSize()], credentials[offset:offset+block.BlockSize()])
	}
	public.FillBytes(response[128:])
	_, err = a.conn.Write(response)
	return err
}
