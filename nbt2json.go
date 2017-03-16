package nbt2json

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// NbtTag represents one NBT tag for each struct
type NbtTag struct {
	TagType byte        `json:"tagType"`
	Name    string      `json:"name"`
	Value   interface{} `json:"value,omitempty"`
}

// NbtTagList represents an NBT tag list
type NbtTagList struct {
	TagListType byte          `json:"tagListType"`
	List        []interface{} `json:"list"`
}

// NbtParseError is when the data does not match an expected pattern. Pass it message string and downstream error
type NbtParseError struct {
	s string
	e error
}

func (e NbtParseError) Error() string {
	var s string
	if e.e != nil {
		s = fmt.Sprintf(": %s", e.e.Error())
	}
	return fmt.Sprintf("Error parsing NBT: %s%s", e.s, s)
}

// Reads 0-8 bytes and returns an int64 value
func readInt(r *bytes.Reader, numBytes int, byteOrder binary.ByteOrder) (i int64, err error) {
	var myInt64 []byte
	temp := make([]byte, numBytes)
	err = binary.Read(r, byteOrder, &temp)
	if err != nil {
		return i, NbtParseError{fmt.Sprintf("Reading %v bytes for intxx", numBytes), err}
	}
	padding := make([]byte, 8-numBytes)
	if byteOrder == binary.BigEndian {
		myInt64 = append(padding, temp...)
	} else if byteOrder == binary.LittleEndian {
		myInt64 = append(temp, padding...)
	} else {
		_ = myInt64
		return i, NbtParseError{"byteOrder not recognized", nil}
	}
	buf := bytes.NewReader(myInt64)
	err = binary.Read(buf, byteOrder, &i)
	return i, err
}

// Nbt2Json ...
func Nbt2Json(r *bytes.Reader, byteOrder binary.ByteOrder) ([]byte, error) {
	var data NbtTag
	err := binary.Read(r, byteOrder, &data.TagType)
	if err != nil {
		return nil, NbtParseError{"Reading TagType", err}
	}
	// do not try to fetch name for TagType 0 which is compound end tag
	if data.TagType != 0 {
		var err error
		var nameLen int64
		nameLen, err = readInt(r, 2, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading Name length", err}
		}
		name := make([]byte, nameLen)
		err = binary.Read(r, byteOrder, &name)
		if err != nil {
			return nil, NbtParseError{"Reading Name - is little/big endian byte order set correctly?", err}
		}
		data.Name = string(name[:])
	}
	data.Value, err = getPayload(r, byteOrder, data.TagType)
	if err != nil {
		return nil, err
	}
	outJson, err := json.MarshalIndent(data, "", "  ")
	return outJson, nil
}

// Gets the tag payload. Had to break this out from the main function to allow tag list recursion
func getPayload(r *bytes.Reader, byteOrder binary.ByteOrder, tagType byte) (interface{}, error) {
	var output interface{}
	var err error
	switch tagType {
	case 0:
		// end tag for compound; do nothing further
	case 1:
		output, err = readInt(r, 1, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading int8", err}
		}
	case 2:
		output, err = readInt(r, 2, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading int16", err}
		}
	case 3:
		output, err = readInt(r, 4, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading int32", err}
		}
	case 4:
		output, err = readInt(r, 8, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading int64", err}
		}
	case 5:
		var f float32
		err = binary.Read(r, byteOrder, &f)
		if err != nil {
			return nil, NbtParseError{"Reading float32", err}
		}
		output = f
	case 6:
		var f float64
		err = binary.Read(r, byteOrder, &f)
		if err != nil {
			return nil, NbtParseError{"Reading float64", err}
		}
		output = f
	case 7:
		var byteArray []byte
		var oneByte byte
		numRecords, err := readInt(r, 4, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading byte array tag length", err}
		}
		for i := int64(1); i <= numRecords; i++ {
			err := binary.Read(r, byteOrder, &oneByte)
			if err != nil {
				return nil, NbtParseError{"Reading byte in byte array tag", err}
			}
			byteArray = append(byteArray, oneByte)
		}
		output = byteArray
	case 8:
		strLen, err := readInt(r, 2, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading string tag length", err}
		}
		utf8String := make([]byte, strLen)
		err = binary.Read(r, byteOrder, &utf8String)
		if err != nil {
			return nil, NbtParseError{"Reading string tag data", err}
		}
		output = string(utf8String[:])
	case 9:
		var tagList NbtTagList
		err = binary.Read(r, byteOrder, &tagList.TagListType)
		if err != nil {
			return nil, NbtParseError{"Reading TagType", err}
		}
		numRecords, err := readInt(r, 4, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading list tag length", err}
		}
		for i := int64(1); i <= numRecords; i++ {
			payload, err := getPayload(r, byteOrder, tagList.TagListType)
			if err != nil {
				return nil, NbtParseError{"Reading list tag item", err}
			}
			tagList.List = append(tagList.List, payload)
		}
		output = tagList
	case 10:
		var compound []json.RawMessage
		var tagtype int64
		for tagtype, err = readInt(r, 1, byteOrder); tagtype != 0; tagtype, err = readInt(r, 1, byteOrder) {
			if err != nil {
				return nil, NbtParseError{"compound: reading next tag type", err}
			}
			_, err = r.Seek(-1, 1)
			if err != nil {
				return nil, NbtParseError{"seeking back one", err}
			}
			tag, err := Nbt2Json(r, byteOrder)
			if err != nil {
				return nil, NbtParseError{"compound: reading a child tag", err}
			}
			compound = append(compound, json.RawMessage(string(tag)))
		}
		output = compound
	case 11:
		var intArray []int32
		var oneInt int32
		numRecords, err := readInt(r, 4, byteOrder)
		if err != nil {
			return nil, NbtParseError{"Reading int array tag length", err}
		}
		for i := int64(1); i <= numRecords; i++ {
			err := binary.Read(r, byteOrder, &oneInt)
			if err != nil {
				return nil, NbtParseError{"Reading int in int array tag", err}
			}
			intArray = append(intArray, oneInt)
		}
		output = intArray
	default:
		return nil, NbtParseError{"TagType not recognized", nil}
	}
	return output, nil
}
