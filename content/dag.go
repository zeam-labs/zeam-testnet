package content

import (
	"encoding/binary"
	"fmt"
)

const DefaultChunkSize = 256 * 1024

const (
	nodeTypeLeaf     byte = 0x00
	nodeTypeInternal byte = 0x01
)

type DAGNode struct {
	ID       ContentID
	Children []ContentID
	Size     uint64
	IsLeaf   bool
}

type DAGBuilder struct {
	store     *Store
	chunkSize int
}

func NewDAGBuilder(store *Store, chunkSize int) *DAGBuilder {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	return &DAGBuilder{
		store:     store,
		chunkSize: chunkSize,
	}
}

func (b *DAGBuilder) Add(data []byte) (ContentID, error) {
	if len(data) == 0 {
		return ContentID{}, fmt.Errorf("cannot add empty content")
	}

	if len(data) <= b.chunkSize {
		encoded := encodeLeaf(data)
		id := b.store.Put(encoded)
		return id, nil
	}

	var childIDs []ContentID
	for offset := 0; offset < len(data); offset += b.chunkSize {
		end := offset + b.chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]
		encoded := encodeLeaf(chunk)
		id := b.store.Put(encoded)
		childIDs = append(childIDs, id)
	}

	encoded := encodeInternal(uint64(len(data)), childIDs)
	id := b.store.Put(encoded)
	return id, nil
}

func (b *DAGBuilder) Get(root ContentID) ([]byte, error) {
	raw, ok := b.store.Get(root)
	if !ok {
		return nil, fmt.Errorf("content not found: %s", root.Hex())
	}

	node, data, err := decodeNode(raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", root.Hex(), err)
	}

	if node.IsLeaf {
		return data, nil
	}

	result := make([]byte, 0, node.Size)
	for _, childID := range node.Children {
		childData, err := b.Get(childID)
		if err != nil {
			return nil, fmt.Errorf("missing chunk %s: %w", childID.Hex(), err)
		}
		result = append(result, childData...)
	}
	return result, nil
}

func (b *DAGBuilder) Manifest(id ContentID) (*DAGNode, error) {
	raw, ok := b.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("content not found: %s", id.Hex())
	}

	node, data, err := decodeNode(raw)
	if err != nil {
		return nil, err
	}
	node.ID = id
	if node.IsLeaf {
		node.Size = uint64(len(data))
	}
	return node, nil
}

func (b *DAGBuilder) MissingChunks(root ContentID) ([]ContentID, error) {
	raw, ok := b.store.Get(root)
	if !ok {
		return []ContentID{root}, nil
	}

	node, _, err := decodeNode(raw)
	if err != nil {
		return nil, err
	}

	if node.IsLeaf {
		return nil, nil
	}

	var missing []ContentID
	for _, childID := range node.Children {
		if !b.store.Has(childID) {
			missing = append(missing, childID)
		}
	}
	return missing, nil
}

func encodeLeaf(data []byte) []byte {
	buf := make([]byte, 1+len(data))
	buf[0] = nodeTypeLeaf
	copy(buf[1:], data)
	return buf
}

func encodeInternal(size uint64, children []ContentID) []byte {
	buf := make([]byte, 1+8+len(children)*32)
	buf[0] = nodeTypeInternal
	binary.BigEndian.PutUint64(buf[1:9], size)
	for i, id := range children {
		copy(buf[9+i*32:9+(i+1)*32], id[:])
	}
	return buf
}

func decodeNode(raw []byte) (*DAGNode, []byte, error) {
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("empty node data")
	}

	switch raw[0] {
	case nodeTypeLeaf:
		data := raw[1:]
		return &DAGNode{IsLeaf: true, Size: uint64(len(data))}, data, nil

	case nodeTypeInternal:
		if len(raw) < 9 {
			return nil, nil, fmt.Errorf("internal node too short")
		}
		size := binary.BigEndian.Uint64(raw[1:9])
		childBytes := raw[9:]
		if len(childBytes)%32 != 0 {
			return nil, nil, fmt.Errorf("invalid internal node: child data not aligned")
		}
		nChildren := len(childBytes) / 32
		children := make([]ContentID, nChildren)
		for i := 0; i < nChildren; i++ {
			copy(children[i][:], childBytes[i*32:(i+1)*32])
		}
		return &DAGNode{
			IsLeaf:   false,
			Size:     size,
			Children: children,
		}, nil, nil

	default:
		return nil, nil, fmt.Errorf("unknown node type: 0x%02x", raw[0])
	}
}
