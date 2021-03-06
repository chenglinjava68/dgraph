/*
 * Copyright 2017-2018 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/dgraph-io/dgraph/types"
	"github.com/dgraph-io/dgraph/x"
	"github.com/stretchr/testify/require"
)

func TestEncodeMemory(t *testing.T) {
	//	if testing.Short() {
	t.Skip("Skipping TestEncodeMemory")
	//	}
	var wg sync.WaitGroup

	for i := 0; i < runtime.NumCPU(); i++ {
		enc := newEncoder()
		n := enc.newNode()
		require.NotNil(t, n)
		for i := 0; i < 15000; i++ {
			enc.AddValue(n, enc.idForAttr(fmt.Sprintf("very long attr name %06d", i)),
				types.ValueForType(types.StringID))
			enc.AddListChild(n, enc.idForAttr(fmt.Sprintf("another long child %06d", i)),
				enc.newNode())
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				var buf bytes.Buffer
				enc.encode(n, &buf)
			}
		}()
	}

	wg.Wait()
}

func TestNormalizeJSONLimit(t *testing.T) {
	// Set default normalize limit.
	x.Config.NormalizeNodeLimit = 1e4

	if testing.Short() {
		t.Skip("Skipping TestNormalizeJSONLimit")
	}

	enc := newEncoder()
	n := enc.newNodeWithAttr(enc.idForAttr("root"))
	require.NotNil(t, n)
	for i := 0; i < 1000; i++ {
		enc.AddValue(n, enc.idForAttr(fmt.Sprintf("very long attr name %06d", i)),
			types.ValueForType(types.StringID))
		child1 := enc.newNodeWithAttr(enc.idForAttr("child1"))
		enc.AddListChild(n, enc.idForAttr("child1"), child1)
		for j := 0; j < 100; j++ {
			enc.AddValue(child1, enc.idForAttr(fmt.Sprintf("long child1 attr %06d", j)),
				types.ValueForType(types.StringID))
		}
		child2 := enc.newNodeWithAttr(enc.idForAttr("child2"))
		enc.AddListChild(n, enc.idForAttr("child2"), child2)
		for j := 0; j < 100; j++ {
			enc.AddValue(child2, enc.idForAttr(fmt.Sprintf("long child2 attr %06d", j)),
				types.ValueForType(types.StringID))
		}
		child3 := enc.newNodeWithAttr(enc.idForAttr("child3"))
		enc.AddListChild(n, enc.idForAttr("child3"), child3)
		for j := 0; j < 100; j++ {
			enc.AddValue(child3, enc.idForAttr(fmt.Sprintf("long child3 attr %06d", j)),
				types.ValueForType(types.StringID))
		}
	}
	_, err := enc.normalize(n)
	require.Error(t, err, "Couldn't evaluate @normalize directive - too many results")
}

func BenchmarkJsonMarshal(b *testing.B) {
	inputStrings := [][]string{
		[]string{"largestring", strings.Repeat("a", 1024)},
		[]string{"smallstring", "abcdef"},
		[]string{"specialchars", "<><>^)(*&(%*&%&^$*&%)(*&)^)"},
	}

	var result []byte

	for _, input := range inputStrings {
		b.Run(fmt.Sprintf("STDJsonMarshal-%s", input[0]), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				result, _ = json.Marshal(input[1])
			}
		})

		b.Run(fmt.Sprintf("stringJsonMarshal-%s", input[0]), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				result = stringJsonMarshal(input[1])
			}
		})
	}

	_ = result
}

func TestStringJsonMarshal(t *testing.T) {
	inputs := []string{
		"",
		"0",
		"true",
		"1.909045927350",
		"nil",
		"null",
		"<&>",
		`quoted"str"ing`,
	}

	for _, input := range inputs {
		gm, err := json.Marshal(input)
		require.NoError(t, err)

		sm := stringJsonMarshal(input)

		require.Equal(t, gm, sm)
	}
}

func TestFastJsonNode(t *testing.T) {
	attrId := uint16(20)
	scalarVal := bytes.Repeat([]byte("a"), 160)
	list := true

	enc := newEncoder()
	fj := enc.newNode()
	enc.setAttr(fj, attrId)
	require.NoError(t, enc.setScalarVal(fj, scalarVal))
	enc.setList(fj, list)

	require.Equal(t, attrId, enc.getAttr(fj))
	sv, err := enc.getScalarVal(fj)
	require.NoError(t, err)
	require.Equal(t, scalarVal, sv)
	require.Equal(t, list, enc.getList(fj))

	fj2 := enc.newNode()
	enc.setAttr(fj2, attrId)
	require.NoError(t, enc.setScalarVal(fj2, scalarVal))
	enc.setList(fj2, list)

	sv, err = enc.getScalarVal(fj2)
	require.NoError(t, err)
	require.Equal(t, scalarVal, sv)
	require.Equal(t, list, enc.getList(fj2))

	enc.appendAttrs(fj, fj2)
	require.Equal(t, []fastJsonNode{fj2}, enc.getAttrs(fj))
}

func BenchmarkFastJsonNodeEmpty(b *testing.B) {
	for i := 0; i < b.N; i++ {
		enc := newEncoder()
		var fj fastJsonNode
		for i := 0; i < 2e6; i++ {
			fj = enc.newNode()
		}
		_ = fj
	}
}

var (
	testAttr = "abcdefghijklmnop"
	testVal  = types.Val{Tid: types.DefaultID, Value: []byte(testAttr)}
)

func buildTestTree(b *testing.B, enc *encoder, level, maxlevel int, fj fastJsonNode) {
	if level >= maxlevel {
		return
	}

	// Add only two children for now.
	for i := 0; i < 2; i++ {
		var ch fastJsonNode
		if level == maxlevel-1 {
			val, err := valToBytes(testVal)
			if err != nil {
				panic(err)
			}

			ch, err = enc.makeScalarNode(enc.idForAttr(testAttr), val, false)
			require.NoError(b, err)
		} else {
			ch := enc.newNodeWithAttr(enc.idForAttr(testAttr))
			buildTestTree(b, enc, level+1, maxlevel, ch)
		}
		enc.appendAttrs(fj, ch)
	}
}

func BenchmarkFastJsonNode2Chilren(b *testing.B) {
	for i := 0; i < b.N; i++ {
		enc := newEncoder()
		root := enc.newNodeWithAttr(enc.idForAttr(testAttr))
		buildTestTree(b, enc, 1, 20, root)
	}
}
