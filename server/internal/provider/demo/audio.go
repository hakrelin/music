package demo

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand"
	"sync"
)

// 合成音频的参数。44.1kHz / 16bit / 单声道是 docs §7 的约定，
// 也是"随便什么浏览器都能播、又足够小"的折中。
const (
	sampleRate  = 44100
	tableSize   = 4096
	audioCacheN = 4 // LRU 容量：4 首 ≈ 4×4MB，控制常驻内存
)

// sineTable 是预计算的单周期正弦表。
//
// 用查表 + 线性插值而不是每样本 math.Sin，是因为一首 45 秒的曲子有 ~200 万个
// 采样点、每个点要叠好几个分音；直接调 math.Sin 会让首次请求明显变慢
// （数百毫秒）。查表后整首合成在几十毫秒量级。
var sineTable = func() []float64 {
	t := make([]float64, tableSize+1)
	for i := 0; i <= tableSize; i++ {
		t[i] = math.Sin(2 * math.Pi * float64(i) / tableSize)
	}
	return t
}()

// audioCache 是进程内的音频缓存（LRU，简单实现：切片记顺序）。
//
// 不缓存的话每个播放请求都要重算一遍；缓存太多又会吃掉几十 MB 内存，
// 所以只留最近用到的几首。前端播完一首就不太可能马上再要更早的曲目。
type audioCache struct {
	mu    sync.Mutex
	items map[string][]byte
	order []string
}

var audio = &audioCache{items: make(map[string][]byte, audioCacheN)}

// synthSem 限制同时合成的曲目数。
//
// 每首 30–45s 的曲子要临时分配 ~8MB 浮点缓冲 + 4MB 输出，并跑几十毫秒
// 的合成循环；如果浏览器同时请求很多首，不设上限会把 CPU/内存打满。
// 2 路并发足够让"进房 + 预加载"不排队太久。
var synthSem = make(chan struct{}, 2)

// Audio 返回曲目的 WAV 字节；id 不在演示曲库里时返回 false。
func (p *Provider) Audio(id string) ([]byte, bool) {
	s, ok := p.byID[id]
	if !ok {
		return nil, false
	}
	if b := audio.get(id); b != nil {
		return b, true
	}

	synthSem <- struct{}{}
	defer func() { <-synthSem }()

	// 排队期间可能已被别的请求生成好了。
	if b := audio.get(id); b != nil {
		return b, true
	}
	b := synthWAV(id, s.Duration)
	audio.put(id, b)
	return b, true
}

func (c *audioCache) get(id string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.items[id]
}

func (c *audioCache) put(id string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[id]; exists {
		c.items[id] = data
		return
	}
	if len(c.order) >= audioCacheN {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.items, oldest)
	}
	c.items[id] = data
	c.order = append(c.order, id)
}

// synthWAV 合成一首 length 秒的柔和和弦垫并编码成 WAV。
func synthWAV(id string, length int) []byte {
	if length <= 0 {
		length = 30
	}
	rnd := rand.New(rand.NewSource(seedOf(id)))
	total := length * sampleRate
	buf := make([]float32, total)

	// 中国五声音阶（宫商角徵羽）的逐半音音程，配合低音持续音，
	// 听起来接近"东方风"的空灵垫底，而不会像随机正弦。
	scale := []int{0, 2, 4, 7, 9, 12, 14, 16, 19}
	roots := []float64{110.0, 123.47, 146.83, 164.81, 196.0, 220.0}
	root := roots[rnd.Intn(len(roots))]

	// 每段 4 秒左右，段与段之间用 1.2 秒的释放尾音交叉淡化，避免爆音。
	segLen := 4 * sampleRate
	nSeg := (total + segLen - 1) / segLen
	if nSeg < 1 {
		nSeg = 1
	}
	// 和弦进行：每段从音阶里挑 3 个音，根音在低八度做持续低音。
	const release = 1.2 * sampleRate
	for seg := 0; seg < nSeg; seg++ {
		start := seg * segLen
		if start >= total {
			break
		}
		degree := (seg*3 + rnd.Intn(2)) % len(scale)
		notes := []float64{
			root / 2, // 低音
			root * ratio(scale[degree]),
			root * ratio(scale[(degree+2)%len(scale)]),
			root * ratio(scale[(degree+4)%len(scale)]),
		}
		amps := []float64{0.5, 0.7, 0.55, 0.4}
		for ni, freq := range notes {
			// 主音 + 一个八度泛音（低音量），让音色不至于太干。
			addTone(buf, float64(start), float64(segLen), release, freq, amps[ni])
			addTone(buf, float64(start), float64(segLen), release, freq*2, amps[ni]*0.18)
		}
	}

	// 轻微噪声：一层几乎听不见的"空气感"，也让波形不完美周期化。
	noise := 0.0
	for i := 0; i < total; i++ {
		noise = noise*0.96 + (rnd.Float64()*2-1)*0.04
		buf[i] += float32(noise * 0.06)
	}

	// 首尾总包络：淡入 2s、淡出 3s，避免点击声。
	fadeIn := 2 * sampleRate
	fadeOut := 3 * sampleRate
	if fadeIn > total/4 {
		fadeIn = total / 4
	}
	if fadeOut > total/4 {
		fadeOut = total / 4
	}
	for i := 0; i < fadeIn && i < total; i++ {
		buf[i] *= float32(i) / float32(fadeIn)
	}
	for i := 0; i < fadeOut && i < total; i++ {
		buf[total-1-i] *= float32(i) / float32(fadeOut)
	}

	// 归一化到 -1.5dB 左右，既不会削波，也不至于太小声。
	peak := 0.0
	for _, v := range buf {
		if a := math.Abs(float64(v)); a > peak {
			peak = a
		}
	}
	if peak > 0 {
		k := 0.84 / peak
		for i := range buf {
			buf[i] = float32(float64(buf[i]) * k)
		}
	}

	return encodeWAV(buf)
}

// addTone 把一个带包络的正弦（及谐波）叠加进 buf。
//
// start/segLen/release 都以样本为单位；尾音会越过段尾，与下一段交叠。
func addTone(buf []float32, start, segLen, release, freq, amp float64) {
	if freq <= 0 || amp <= 0 {
		return
	}
	inc := freq / sampleRate // 每样本相位增量（周期数）
	phase := 0.0
	total := len(buf)
	end := start + float64(segLen) + release
	if end > float64(total) {
		end = float64(total)
	}
	// 每段内做 0.5 秒淡入、1.2 秒淡出，段与段交叠成连续的音垫。
	attack := 0.5 * sampleRate
	sustainUntil := float64(segLen)
	for i := start; i < end; i++ {
		t := i - start
		var env float64
		if t < attack {
			env = t / attack
		} else {
			env = 1
		}
		if t > sustainUntil {
			env *= (end - i) / (end - sustainUntil)
		}
		if env <= 0 {
			break
		}
		// 查表 + 线性插值
		x := phase * tableSize
		idx := int(x)
		frac := x - float64(idx)
		v := sineTable[idx] + (sineTable[idx+1]-sineTable[idx])*frac
		buf[int(i)] += float32(v * amp * env * 0.5)
		phase += inc
		if phase >= 1 {
			phase -= 1
		}
	}
}

// ratio 把半音音程转成频率倍率。
func ratio(semitones int) float64 {
	return math.Pow(2, float64(semitones)/12)
}

// seedOf 从曲目 ID 派生确定性随机种子：同一首曲子每次合成的结果一致，
// 缓存失效后重新生成也不会"变调"。
func seedOf(id string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return int64(h.Sum64() & 0x7fffffff)
}

// encodeWAV 把 [-1,1] 的浮点样本编码成 16bit 单声道 PCM WAV。
func encodeWAV(samples []float32) []byte {
	dataLen := len(samples) * 2
	b := make([]byte, 44+dataLen)
	copy(b[0:4], "RIFF")
	binary.LittleEndian.PutUint32(b[4:8], uint32(36+dataLen))
	copy(b[8:12], "WAVE")
	copy(b[12:16], "fmt ")
	binary.LittleEndian.PutUint32(b[16:20], 16)             // fmt chunk 大小
	binary.LittleEndian.PutUint16(b[20:22], 1)              // PCM
	binary.LittleEndian.PutUint16(b[22:24], 1)              // 单声道
	binary.LittleEndian.PutUint32(b[24:28], sampleRate)     // 采样率
	binary.LittleEndian.PutUint32(b[28:32], sampleRate*1*2) // 字节率
	binary.LittleEndian.PutUint16(b[32:34], 2)              // 块对齐
	binary.LittleEndian.PutUint16(b[34:36], 16)             // 位深
	copy(b[36:40], "data")
	binary.LittleEndian.PutUint32(b[40:44], uint32(dataLen))
	for i, s := range samples {
		v := float64(s) * 32767
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(int16(v)))
	}
	return b
}
