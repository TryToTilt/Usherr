package link

import "crypto/ed25519"

// ForgeMessage seals m for the pair (me, peer) but signs it with signer's
// key: an envelope whose signature isn't the sender's.
func ForgeMessage(me, signer *Identity, peerSign ed25519.PublicKey, peerX []byte, m Message) ([]byte, error) {
	mb := []byte(`{"type":"` + m.Type + `","id":"` + m.ID + `","at":` + itoa(m.At) + `,"body":{}}`)
	plain := []byte(`{"m":` + string(mb) + `,"sig":"` + b64(signer.Sign(mb)) + `"}`)
	key, err := pairKey(me, peerSign, peerX)
	if err != nil {
		return nil, err
	}
	return seal(key, plain, nil)
}
