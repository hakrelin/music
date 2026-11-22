// Package qrcode 提供零外部依赖的 ISO/IEC 18004 二维码编码与 SVG 渲染。
//
// 支持字节模式（Byte Mode）、纠错等级 M、版本 1–12（最长 287 字节），
// 覆盖网易云和 B 站扫码登录链接。
package qrcode

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Matrix 表示一个正方形二维码模块矩阵（true 表示深色模块）。
type Matrix struct {
	Version int
	Size    int
	Modules [][]bool
}

type versionSpec struct {
	version    int
	totalCode  int
	ecPerBlock int
	g1Blocks   int
	g1Data     int
	g2Blocks   int
	g2Data     int
	align      []int
}

// ISO/IEC 18004 Table 9 — EC Level M, Versions 1..12.
var specsM = []versionSpec{
	{version: 1, totalCode: 26, ecPerBlock: 10, g1Blocks: 1, g1Data: 16},
	{version: 2, totalCode: 44, ecPerBlock: 16, g1Blocks: 1, g1Data: 28, align: []int{6, 18}},
	{version: 3, totalCode: 70, ecPerBlock: 26, g1Blocks: 1, g1Data: 44, align: []int{6, 22}},
	{version: 4, totalCode: 100, ecPerBlock: 18, g1Blocks: 2, g1Data: 32, align: []int{6, 26}},
	{version: 5, totalCode: 134, ecPerBlock: 24, g1Blocks: 2, g1Data: 43, align: []int{6, 30}},
	{version: 6, totalCode: 172, ecPerBlock: 16, g1Blocks: 4, g1Data: 27, align: []int{6, 34}},
	{version: 7, totalCode: 196, ecPerBlock: 18, g1Blocks: 4, g1Data: 31, align: []int{6, 22, 38}},
	{version: 8, totalCode: 242, ecPerBlock: 22, g1Blocks: 2, g1Data: 38, g2Blocks: 2, g2Data: 39, align: []int{6, 24, 42}},
	{version: 9, totalCode: 292, ecPerBlock: 22, g1Blocks: 3, g1Data: 36, g2Blocks: 2, g2Data: 37, align: []int{6, 26, 46}},
	{version: 10, totalCode: 346, ecPerBlock: 26, g1Blocks: 4, g1Data: 43, g2Blocks: 1, g2Data: 44, align: []int{6, 28, 50}},
	{version: 11, totalCode: 404, ecPerBlock: 30, g1Blocks: 1, g1Data: 50, g2Blocks: 4, g2Data: 51, align: []int{6, 30, 54}},
	{version: 12, totalCode: 466, ecPerBlock: 22, g1Blocks: 6, g1Data: 36, g2Blocks: 2, g2Data: 37, align: []int{6, 32, 58}},
}

func (s versionSpec) dataCodewords() int {
	return s.g1Blocks*s.g1Data + s.g2Blocks*s.g2Data
}

func (s versionSpec) countBits() int {
	if s.version >= 10 {
		return 16
	}
	return 8
}

func (s versionSpec) byteCapacity() int {
	return (s.dataCodewords()*8 - 4 - s.countBits()) / 8
}

// Encode 将文本编码为二维码矩阵（Byte 模式，EC Level M）。
func Encode(text string) (*Matrix, error) {
	data := []byte(text)
	if len(data) == 0 {
		return nil, errors.New("qrcode: 内容不能为空")
	}
	var spec versionSpec
	found := false
	for _, s := range specsM {
		if len(data) <= s.byteCapacity() {
			spec = s
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("qrcode: 内容过长（%d 字节，上限 %d 字节）", len(data), specsM[len(specsM)-1].byteCapacity())
	}

	codewords := buildCodewords(data, spec)
	size := spec.version*4 + 17

	base := makeBoolGrid(size)
	isFunc := makeBoolGrid(size)
	drawFunctionPatterns(base, isFunc, spec)
	placeDataBits(base, isFunc, codewords)

	bestMask := 0
	bestPenalty := int(^uint(0) >> 1)
	var best [][]bool
	for mask := 0; mask < 8; mask++ {
		cand := cloneGrid(base)
		applyMask(cand, isFunc, mask)
		drawFormatBits(cand, mask)
		if spec.version >= 7 {
			drawVersionBits(cand, spec.version)
		}
		if p := penaltyScore(cand); p < bestPenalty {
			bestPenalty = p
			bestMask = mask
			best = cand
		}
	}
	_ = bestMask
	return &Matrix{
		Version: spec.version,
		Size:    size,
		Modules: best,
	}, nil
}

// SVG 返回带 4 模块静区的清晰 SVG 文本。
func (m *Matrix) SVG() string {
	const quiet = 4
	dim := m.Size + quiet*2
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, dim, dim)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#ffffff"/>`, dim, dim)
	b.WriteString(`<path fill="#111111" d="`)
	for y := 0; y < m.Size; y++ {
		x := 0
		for x < m.Size {
			if !m.Modules[y][x] {
				x++
				continue
			}
			run := 1
			for x+run < m.Size && m.Modules[y][x+run] {
				run++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", x+quiet, y+quiet, run, run)
			x += run
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

// SVGDataURL 返回可直接放入 <img src> 的 data URL。
func SVGDataURL(text string) (string, error) {
	m, err := Encode(text)
	if err != nil {
		return "", err
	}
	svg := m.SVG()
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg)), nil
}

// ---- 数据编码与 Reed-Solomon 纠错 ----

type bitBuf struct {
	bits []byte
}

func (b *bitBuf) append(val int, length int) {
	for i := length - 1; i >= 0; i-- {
		b.bits = append(b.bits, byte((val>>i)&1))
	}
}

func buildCodewords(data []byte, spec versionSpec) []byte {
	var buf bitBuf
	// Byte mode indicator = 0100
	buf.append(0b0100, 4)
	buf.append(len(data), spec.countBits())
	for _, ch := range data {
		buf.append(int(ch), 8)
	}

	maxBits := spec.dataCodewords() * 8
	// Terminator (up to 4 zero bits)
	term := maxBits - len(buf.bits)
	if term > 4 {
		term = 4
	}
	if term > 0 {
		buf.append(0, term)
	}
	// Pad to byte boundary
	if rem := len(buf.bits) % 8; rem != 0 {
		buf.append(0, 8-rem)
	}

	rawData := make([]byte, 0, spec.dataCodewords())
	for i := 0; i < len(buf.bits); i += 8 {
		var v byte
		for j := 0; j < 8; j++ {
			v = (v << 1) | buf.bits[i+j]
		}
		rawData = append(rawData, v)
	}
	// Pad codewords 0xEC, 0x11 alternately
	pad := []byte{0xEC, 0x11}
	for i := 0; len(rawData) < spec.dataCodewords(); i++ {
		rawData = append(rawData, pad[i%2])
	}

	// Split into blocks and compute Reed-Solomon EC for each block
	totalBlocks := spec.g1Blocks + spec.g2Blocks
	dataBlocks := make([][]byte, 0, totalBlocks)
	ecBlocks := make([][]byte, 0, totalBlocks)
	gen := rsGenerator(spec.ecPerBlock)

	off := 0
	for i := 0; i < spec.g1Blocks; i++ {
		blk := rawData[off : off+spec.g1Data]
		off += spec.g1Data
		dataBlocks = append(dataBlocks, blk)
		ecBlocks = append(ecBlocks, rsRemainder(blk, gen))
	}
	for i := 0; i < spec.g2Blocks; i++ {
		blk := rawData[off : off+spec.g2Data]
		off += spec.g2Data
		dataBlocks = append(dataBlocks, blk)
		ecBlocks = append(ecBlocks, rsRemainder(blk, gen))
	}

	// Interleave data codewords, then EC codewords
	out := make([]byte, 0, spec.totalCode)
	maxDataLen := spec.g1Data
	if spec.g2Blocks > 0 && spec.g2Data > maxDataLen {
		maxDataLen = spec.g2Data
	}
	for i := 0; i < maxDataLen; i++ {
		for _, blk := range dataBlocks {
			if i < len(blk) {
				out = append(out, blk[i])
			}
		}
	}
	for i := 0; i < spec.ecPerBlock; i++ {
		for _, ec := range ecBlocks {
			out = append(out, ec[i])
		}
	}
	return out
}

// GF(256) with primitive polynomial x^8 + x^4 + x^3 + x^2 + 1 (0x11d)
func gfMul(x, y byte) byte {
	var z byte
	for i := 7; i >= 0; i-- {
		hi := z & 0x80
		z <<= 1
		if hi != 0 {
			z ^= 0x1d
		}
		if (y>>i)&1 != 0 {
			z ^= x
		}
	}
	return z
}

func rsGenerator(degree int) []byte {
	poly := make([]byte, degree)
	poly[degree-1] = 1
	var root byte = 1
	for i := 0; i < degree; i++ {
		for j := 0; j < degree; j++ {
			poly[j] = gfMul(poly[j], root)
			if j+1 < degree {
				poly[j] ^= poly[j+1]
			}
		}
		root = gfMul(root, 0x02)
	}
	return poly
}

func rsRemainder(data, gen []byte) []byte {
	rem := make([]byte, len(gen))
	for _, b := range data {
		factor := b ^ rem[0]
		copy(rem, rem[1:])
		rem[len(rem)-1] = 0
		for i, g := range gen {
			rem[i] ^= gfMul(g, factor)
		}
	}
	return rem
}

// ---- 矩阵布局 ----

func makeBoolGrid(size int) [][]bool {
	g := make([][]bool, size)
	for i := range g {
		g[i] = make([]bool, size)
	}
	return g
}

func cloneGrid(src [][]bool) [][]bool {
	out := make([][]bool, len(src))
	for i := range src {
		out[i] = append([]bool(nil), src[i]...)
	}
	return out
}

func setFunc(mod, isFunc [][]bool, x, y int, dark bool) {
	if y >= 0 && y < len(mod) && x >= 0 && x < len(mod) {
		mod[y][x] = dark
		isFunc[y][x] = true
	}
}

func drawFinder(mod, isFunc [][]bool, cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			dist := absInt(dx)
			if ay := absInt(dy); ay > dist {
				dist = ay
			}
			dark := dist != 2 && dist != 4
			setFunc(mod, isFunc, cx+dx, cy+dy, dark)
		}
	}
}

func drawAlignment(mod, isFunc [][]bool, cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			dist := absInt(dx)
			if ay := absInt(dy); ay > dist {
				dist = ay
			}
			setFunc(mod, isFunc, cx+dx, cy+dy, dist != 1)
		}
	}
}

func drawFunctionPatterns(mod, isFunc [][]bool, spec versionSpec) {
	size := len(mod)
	// Timing patterns
	for i := 0; i < size; i++ {
		setFunc(mod, isFunc, 6, i, i%2 == 0)
		setFunc(mod, isFunc, i, 6, i%2 == 0)
	}
	// Finder patterns + separators
	drawFinder(mod, isFunc, 3, 3)
	drawFinder(mod, isFunc, size-4, 3)
	drawFinder(mod, isFunc, 3, size-4)

	// Alignment patterns
	n := len(spec.align)
	for i, cy := range spec.align {
		for j, cx := range spec.align {
			if (i == 0 && j == 0) || (i == 0 && j == n-1) || (i == n-1 && j == 0) {
				continue
			}
			drawAlignment(mod, isFunc, cx, cy)
		}
	}

	// Reserve format info areas (filled after masking); keep timing modules at row/col 6 intact.
	for i := 0; i <= 8; i++ {
		if i == 6 {
			continue
		}
		setFunc(mod, isFunc, 8, i, false)
		setFunc(mod, isFunc, i, 8, false)
	}
	for i := 0; i < 8; i++ {
		setFunc(mod, isFunc, size-1-i, 8, false)
	}
	for i := 0; i < 7; i++ {
		setFunc(mod, isFunc, 8, size-1-i, false)
	}
	// Dark module
	setFunc(mod, isFunc, 8, size-8, true)

	// Reserve version info areas for V >= 7
	if spec.version >= 7 {
		for i := 0; i < 6; i++ {
			for j := 0; j < 3; j++ {
				setFunc(mod, isFunc, size-11+j, i, false)
				setFunc(mod, isFunc, i, size-11+j, false)
			}
		}
	}
}

func placeDataBits(mod, isFunc [][]bool, codewords []byte) {
	size := len(mod)
	bitIdx := 0
	totalBits := len(codewords) * 8

	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		upward := ((right + 1) & 2) == 0
		for vert := 0; vert < size; vert++ {
			y := vert
			if upward {
				y = size - 1 - vert
			}
			for j := 0; j < 2; j++ {
				x := right - j
				if isFunc[y][x] {
					continue
				}
				dark := false
				if bitIdx < totalBits {
					dark = ((codewords[bitIdx>>3] >> (7 - (bitIdx & 7))) & 1) != 0
					bitIdx++
				}
				mod[y][x] = dark
			}
		}
	}
}

func applyMask(mod, isFunc [][]bool, mask int) {
	size := len(mod)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if isFunc[y][x] {
				continue
			}
			var invert bool
			switch mask {
			case 0:
				invert = (x+y)%2 == 0
			case 1:
				invert = y%2 == 0
			case 2:
				invert = x%3 == 0
			case 3:
				invert = (x+y)%3 == 0
			case 4:
				invert = (y/2+x/3)%2 == 0
			case 5:
				invert = (x*y)%2+(x*y)%3 == 0
			case 6:
				invert = ((x*y)%2+(x*y)%3)%2 == 0
			case 7:
				invert = ((x+y)%2+(x*y)%3)%2 == 0
			}
			if invert {
				mod[y][x] = !mod[y][x]
			}
		}
	}
}

// EC Level M format indicator bits = 00 (0).
func drawFormatBits(mod [][]bool, mask int) {
	size := len(mod)
	data := (0b00 << 3) | (mask & 7)
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := ((data << 10) | rem) ^ 0x5412

	// First copy: around top-left finder
	for i := 0; i <= 5; i++ {
		mod[i][8] = ((bits >> i) & 1) != 0
	}
	mod[7][8] = ((bits >> 6) & 1) != 0
	mod[8][8] = ((bits >> 7) & 1) != 0
	mod[8][7] = ((bits >> 8) & 1) != 0
	for i := 9; i < 15; i++ {
		mod[8][14-i] = ((bits >> i) & 1) != 0
	}

	// Second copy: bottom-left and top-right
	for i := 0; i < 8; i++ {
		mod[8][size-1-i] = ((bits >> i) & 1) != 0
	}
	for i := 8; i < 15; i++ {
		mod[size-15+i][8] = ((bits >> i) & 1) != 0
	}
	mod[size-8][8] = true
}

func drawVersionBits(mod [][]bool, version int) {
	size := len(mod)
	rem := version
	for i := 0; i < 12; i++ {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1f25)
	}
	bits := (version << 12) | rem
	for i := 0; i < 18; i++ {
		dark := ((bits >> i) & 1) != 0
		a := size - 11 + i%3
		b := i / 3
		mod[b][a] = dark
		mod[a][b] = dark
	}
}

// ---- 掩码惩罚分（ISO/IEC 18004 §7.8.3.1） ----

func penaltyScore(mod [][]bool) int {
	size := len(mod)
	score := 0

	// Rule 1: adjacent modules in row/col in same color
	for y := 0; y < size; y++ {
		runColor := mod[y][0]
		runLen := 1
		for x := 1; x < size; x++ {
			if mod[y][x] == runColor {
				runLen++
				if runLen == 5 {
					score += 3
				} else if runLen > 5 {
					score++
				}
			} else {
				runColor = mod[y][x]
				runLen = 1
			}
		}
	}
	for x := 0; x < size; x++ {
		runColor := mod[0][x]
		runLen := 1
		for y := 1; y < size; y++ {
			if mod[y][x] == runColor {
				runLen++
				if runLen == 5 {
					score += 3
				} else if runLen > 5 {
					score++
				}
			} else {
				runColor = mod[y][x]
				runLen = 1
			}
		}
	}

	// Rule 2: 2x2 blocks of same color
	for y := 0; y < size-1; y++ {
		for x := 0; x < size-1; x++ {
			c := mod[y][x]
			if c == mod[y][x+1] && c == mod[y+1][x] && c == mod[y+1][x+1] {
				score += 3
			}
		}
	}

	// Rule 3: finder-like patterns 1:1:3:1:1 with 4 light modules on either side
	for y := 0; y < size; y++ {
		for x := 0; x <= size-11; x++ {
			if matchFinderLikeRow(mod, y, x) {
				score += 40
			}
		}
	}
	for x := 0; x < size; x++ {
		for y := 0; y <= size-11; y++ {
			if matchFinderLikeCol(mod, x, y) {
				score += 40
			}
		}
	}

	// Rule 4: balance of dark/light modules
	dark := 0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if mod[y][x] {
				dark++
			}
		}
	}
	total := size * size
	k := absInt(dark*20-total*10) / total
	score += k * 10
	return score
}

func matchFinderLikeRow(mod [][]bool, y, x int) bool {
	p1 := mod[y][x] && !mod[y][x+1] && mod[y][x+2] && mod[y][x+3] && mod[y][x+4] && !mod[y][x+5] && mod[y][x+6] &&
		!mod[y][x+7] && !mod[y][x+8] && !mod[y][x+9] && !mod[y][x+10]
	p2 := !mod[y][x] && !mod[y][x+1] && !mod[y][x+2] && !mod[y][x+3] &&
		mod[y][x+4] && !mod[y][x+5] && mod[y][x+6] && mod[y][x+7] && mod[y][x+8] && !mod[y][x+9] && mod[y][x+10]
	return p1 || p2
}

func matchFinderLikeCol(mod [][]bool, x, y int) bool {
	p1 := mod[y][x] && !mod[y+1][x] && mod[y+2][x] && mod[y+3][x] && mod[y+4][x] && !mod[y+5][x] && mod[y+6][x] &&
		!mod[y+7][x] && !mod[y+8][x] && !mod[y+9][x] && !mod[y+10][x]
	p2 := !mod[y][x] && !mod[y+1][x] && !mod[y+2][x] && !mod[y+3][x] &&
		mod[y+4][x] && !mod[y+5][x] && mod[y+6][x] && mod[y+7][x] && mod[y+8][x] && !mod[y+9][x] && mod[y+10][x]
	return p1 || p2
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
