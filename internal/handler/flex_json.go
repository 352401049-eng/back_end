package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexUInt8 兼容 JSON 中的 0/1、true/false、"1"/"0"。
type FlexUInt8 uint8

func (f *FlexUInt8) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}
	var n uint8
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexUInt8(n)
		return nil
	}
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*f = FlexUInt8(i)
		return nil
	}
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*f = 1
		} else {
			*f = 0
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.ParseUint(s, 10, 8)
		if err != nil {
			return fmt.Errorf("invalid uint8 string %q", s)
		}
		*f = FlexUInt8(v)
		return nil
	}
	return fmt.Errorf("invalid uint8 value: %s", string(data))
}

func (f FlexUInt8) Uint8() uint8 {
	return uint8(f)
}

// FlexUInt32 兼容数字字符串。
type FlexUInt32 uint32

func (f *FlexUInt32) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}
	var n uint32
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexUInt32(n)
		return nil
	}
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		if i < 0 {
			return fmt.Errorf("invalid uint32: %d", i)
		}
		*f = FlexUInt32(i)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid uint32 string %q", s)
		}
		*f = FlexUInt32(v)
		return nil
	}
	return fmt.Errorf("invalid uint32 value: %s", string(data))
}

func (f FlexUInt32) Uint32() uint32 {
	return uint32(f)
}

// FlexFloat64 兼容数字字符串。
type FlexFloat64 float64

func (f *FlexFloat64) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexFloat64(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("invalid float string %q", s)
		}
		*f = FlexFloat64(v)
		return nil
	}
	return fmt.Errorf("invalid float value: %s", string(data))
}

func (f FlexFloat64) Float64() float64 {
	return float64(f)
}

// FlexUInt64 兼容数字字符串。
type FlexUInt64 uint64

func (f *FlexUInt64) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}
	var n uint64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexUInt64(n)
		return nil
	}
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		if i < 0 {
			return fmt.Errorf("invalid uint64: %d", i)
		}
		*f = FlexUInt64(i)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid uint64 string %q", s)
		}
		*f = FlexUInt64(v)
		return nil
	}
	return fmt.Errorf("invalid uint64 value: %s", string(data))
}

func (f FlexUInt64) Uint64() uint64 {
	return uint64(f)
}

// FlexInt 兼容数字字符串。
type FlexInt int

func (f *FlexInt) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("invalid int string %q", s)
		}
		*f = FlexInt(v)
		return nil
	}
	return fmt.Errorf("invalid int value: %s", string(data))
}

func (f FlexInt) Int() int {
	return int(f)
}

// FlexNullableFloat64 PATCH 可选浮点：未传 Present=false；传 null 表示清空；传数值表示设置。
type FlexNullableFloat64 struct {
	Present bool
	Null    bool
	Value   float64
}

func (f *FlexNullableFloat64) UnmarshalJSON(data []byte) error {
	f.Present = true
	if string(data) == "null" {
		f.Null = true
		f.Value = 0
		return nil
	}
	f.Null = false
	var flex FlexFloat64
	if err := flex.UnmarshalJSON(data); err != nil {
		return err
	}
	f.Value = flex.Float64()
	return nil
}

func (f FlexNullableFloat64) Ptr() *float64 {
	if !f.Present || f.Null {
		return nil
	}
	v := f.Value
	return &v
}

// FlexNullableString PATCH 可选字符串：未传 Present=false；传 null 表示清空。
type FlexNullableString struct {
	Present bool
	Null    bool
	Value   string
}

func (f *FlexNullableString) UnmarshalJSON(data []byte) error {
	f.Present = true
	if string(data) == "null" {
		f.Null = true
		f.Value = ""
		return nil
	}
	f.Null = false
	return json.Unmarshal(data, &f.Value)
}

func (f FlexNullableString) Ptr() *string {
	if !f.Present || f.Null {
		return nil
	}
	v := f.Value
	return &v
}

// FlexFloat64Ptr 可选浮点，兼容字符串。
type FlexFloat64Ptr struct {
	Set   bool
	Value float64
}

func (f *FlexFloat64Ptr) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		f.Set = false
		f.Value = 0
		return nil
	}
	f.Set = true
	var flex FlexFloat64
	if err := flex.UnmarshalJSON(data); err != nil {
		return err
	}
	f.Value = flex.Float64()
	return nil
}

func (f FlexFloat64Ptr) Ptr() *float64 {
	if !f.Set {
		return nil
	}
	v := f.Value
	return &v
}

// FlexUInt32Ptr 可选整数，兼容字符串。
type FlexUInt32Ptr struct {
	Set   bool
	Value uint32
}

func (f *FlexUInt32Ptr) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		f.Set = false
		f.Value = 0
		return nil
	}
	f.Set = true
	var flex FlexUInt32
	if err := flex.UnmarshalJSON(data); err != nil {
		return err
	}
	f.Value = flex.Uint32()
	return nil
}

func (f FlexUInt32Ptr) Ptr() *uint32 {
	if !f.Set {
		return nil
	}
	v := f.Value
	return &v
}

// FlexUInt8Ptr 可选 0/1，兼容布尔与字符串。
type FlexUInt8Ptr struct {
	Set   bool
	Value uint8
}

func (f *FlexUInt8Ptr) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		f.Set = false
		f.Value = 0
		return nil
	}
	f.Set = true
	var flex FlexUInt8
	if err := flex.UnmarshalJSON(data); err != nil {
		return err
	}
	f.Value = flex.Uint8()
	return nil
}

func (f FlexUInt8Ptr) Ptr() *uint8 {
	if !f.Set {
		return nil
	}
	v := f.Value
	return &v
}

// FlexIntPtr 可选整数，兼容字符串。
type FlexIntPtr struct {
	Set   bool
	Value int
}

func (f *FlexIntPtr) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		f.Set = false
		f.Value = 0
		return nil
	}
	f.Set = true
	var flex FlexInt
	if err := flex.UnmarshalJSON(data); err != nil {
		return err
	}
	f.Value = flex.Int()
	return nil
}

func (f FlexIntPtr) Ptr() *int {
	if !f.Set {
		return nil
	}
	v := f.Value
	return &v
}
