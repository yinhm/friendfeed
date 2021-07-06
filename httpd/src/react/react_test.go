package react

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/flosch/pongo2"
	"github.com/stretchr/testify/assert"
)

var input = `let foo = 1;
<div>
	Hello JSX!
	The value of foo is {foo}.
</div>`

var opts = map[string]interface{}{
	"plugins": []string{
		"transform-react-jsx",
		"transform-block-scoping",
	},
}

func TestTransform(t *testing.T) {
	InitBabel()

	expectedOutput := strings.Join([]string{
		"var foo = 1;",
		"",
		"/*#__PURE__*/",
		`React.createElement("div", null, "Hello JSX! The value of foo is ", foo, ".");`,
	}, "\n")

	for i := 0; i < 10; i++ {
		output, err := Transform(strings.NewReader(input), opts)
		assert.Nil(t, err)
		var outputBuf bytes.Buffer
		io.Copy(&outputBuf, output)
		assert.Equal(t, expectedOutput, outputBuf.String())
	}

	jsx, _ := NewJSX()
	component, err := jsx.TransformFile("../../templates/_feed.jsx")
	assert.Nil(t, err)
	assert.Contains(t, component, "handleLike")

	rc, err := NewReact()
	assert.Nil(t, err)
	err = rc.Load([]byte(component))
	assert.Nil(t, err)

	data := pongo2.Context{
		"show_share":  true,
		"title":       "title",
		"name":        "name",
		"prev_start":  1,
		"next_start":  50,
		"show_paging": true,
	}
	_, err = rc.RenderComponent("Feed", data)
	assert.Nil(t, err)
}
