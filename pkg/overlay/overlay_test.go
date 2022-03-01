package overlay

import (
	"io"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestOverlay(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "overlay")
}

var _ = Describe("OverlayReader", func() {
	testCases := []struct {
		Name     string
		Offset   int64
		Length   int64
		Expected string
	}{
		{
			Name:     "at start",
			Offset:   0,
			Length:   4,
			Expected: "overefghij",
		},
		{
			Name:     "in middle",
			Offset:   3,
			Length:   4,
			Expected: "abcoverhij",
		},
		{
			Name:     "at end",
			Offset:   6,
			Length:   4,
			Expected: "abcdefover",
		},
		{
			Name:     "across end",
			Offset:   8,
			Length:   4,
			Expected: "abcdefghover",
		},
		{
			Name:     "beyond end",
			Offset:   10,
			Length:   4,
			Expected: "abcdefghijover",
		},
		{
			Name:     "empty at start",
			Offset:   0,
			Length:   0,
			Expected: "abcdefghij",
		},
		{
			Name:     "empty in middle",
			Offset:   5,
			Length:   0,
			Expected: "abcdefghij",
		},
		{
			Name:     "empty at end",
			Offset:   9,
			Length:   0,
			Expected: "abcdefghij",
		},
		{
			Name:     "empty over end",
			Offset:   10,
			Length:   0,
			Expected: "abcdefghij",
		},
	}

	It("passes all test cases", func() {
		for _, tc := range testCases {
			By(tc.Name)

			base := "abcdefghij"
			overlayString := "overlay"

			overlay := Overlay{
				Reader: strings.NewReader(overlayString),
				Offset: tc.Offset,
				Length: tc.Length,
			}
			reader, err := NewOverlayReader(strings.NewReader(base), overlay)
			Expect(err).NotTo(HaveOccurred())

			output, err := io.ReadAll(reader)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(Equal(tc.Expected))

			newOffset, err := reader.Seek(3, io.SeekStart)
			Expect(err).NotTo(HaveOccurred())
			Expect(newOffset).To(Equal(int64(3)))

			rangeOutput := make([]byte, 6)
			_, err = io.ReadFull(reader, rangeOutput)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(rangeOutput)).To(Equal(tc.Expected[3:9]))
		}
	})
})

// A reader that returns EOF in the same call as the last bytes
type earlyEOFReader struct {
	data []byte
}

func (r *earlyEOFReader) Read(p []byte) (int, error) {
	num := copy(p, r.data)
	return num, io.EOF
}

func (r *earlyEOFReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		return 0, nil
	case io.SeekCurrent:
		return 0, nil
	case io.SeekEnd:
		return int64(len(r.data)), nil
	default:
		return 0, nil
	}
}

var _ = Describe("AppendReader", func() {
	It("Appends strings", func() {
		base := "abcdefghij"
		appendString := "overlay"
		expected := base + appendString

		reader, err := NewAppendReader(strings.NewReader(base), strings.NewReader(appendString))
		Expect(err).NotTo(HaveOccurred())

		output, err := io.ReadAll(reader)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(output)).To(Equal(expected))
	})
	It("Doesn't error early if the base returns EOF with the last bytes", func() {
		base := "abcdefghij"
		appendString := "overlay"
		reader, err := NewAppendReader(&earlyEOFReader{data: []byte(base)}, strings.NewReader(appendString))
		Expect(err).NotTo(HaveOccurred())

		// enough to get past the base, but not to the end of the expected output
		buf := make([]byte, 14)
		_, err = reader.Read(buf)
		Expect(err).NotTo(HaveOccurred())
	})
})
