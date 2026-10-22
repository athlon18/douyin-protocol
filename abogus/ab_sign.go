package abogus

import (
	"crypto/rc4"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// min 返回两个整数中的较小值（Go 1.21 兼容）
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SM3 国密哈希算法实现
type SM3 struct {
	reg   []uint32
	chunk []byte
	size  int
}

func NewSM3() *SM3 {
	sm3 := &SM3{}
	sm3.Reset()
	return sm3
}

func (sm3 *SM3) Reset() {
	sm3.reg = []uint32{
		1937774191, 1226093241, 388252375, 3666478592,
		2842636476, 372324522, 3817729613, 2969243214,
	}
	sm3.chunk = make([]byte, 0, 64)
	sm3.size = 0
}

func leftRotate(x uint32, n int) uint32 {
	n %= 32
	return (x << n) | (x >> (32 - n))
}

func getTj(j int) uint32 {
	if 0 <= j && j < 16 {
		return 2043430169 // 0x79CC4519
	} else if 16 <= j && j < 64 {
		return 2055708042 // 0x7A879D8A
	}
	// j 应该在 0-63 范围内，这是内部不变量
	return 0
}

func ffj(j int, x, y, z uint32) uint32 {
	if 0 <= j && j < 16 {
		return x ^ y ^ z
	} else if 16 <= j && j < 64 {
		return (x & y) | (x & z) | (y & z)
	}
	// j 应该在 0-63 范围内，这是内部不变量
	return 0
}

func ggj(j int, x, y, z uint32) uint32 {
	if 0 <= j && j < 16 {
		return x ^ y ^ z
	} else if 16 <= j && j < 64 {
		return (x & y) | (^x & z)
	}
	// j 应该在 0-63 范围内，这是内部不变量
	return 0
}

func (sm3 *SM3) Write(data []byte) {
	sm3.size += len(data)
	f := 64 - len(sm3.chunk)

	if len(data) < f {
		sm3.chunk = append(sm3.chunk, data...)
		return
	}

	sm3.chunk = append(sm3.chunk, data[:f]...)
	for len(sm3.chunk) >= 64 {
		sm3.compress(sm3.chunk)
		if f < len(data) {
			end := min(f+64, len(data))
			sm3.chunk = data[f:end]
		} else {
			sm3.chunk = []byte{}
		}
		f += 64
	}
}

func (sm3 *SM3) fill() {
	bitLength := uint64(sm3.size) * 8
	sm3.chunk = append(sm3.chunk, 0x80)

	paddingPos := len(sm3.chunk) % 64
	if 64-paddingPos < 8 {
		for len(sm3.chunk)%64 != 0 {
			sm3.chunk = append(sm3.chunk, 0)
		}
	}

	for len(sm3.chunk)%64 < 56 {
		sm3.chunk = append(sm3.chunk, 0)
	}

	highBits := uint32(bitLength >> 32)
	sm3.chunk = append(sm3.chunk, byte(highBits>>24), byte(highBits>>16), byte(highBits>>8), byte(highBits))

	lowBits := uint32(bitLength & 0xFFFFFFFF)
	sm3.chunk = append(sm3.chunk, byte(lowBits>>24), byte(lowBits>>16), byte(lowBits>>8), byte(lowBits))
}

func (sm3 *SM3) compress(data []byte) {
	if len(data) < 64 {
		// 数据不足 64 字节，内部错误，返回
		return
	}

	w := make([]uint32, 132)
	for t := 0; t < 16; t++ {
		w[t] = binary.BigEndian.Uint32(data[4*t : 4*t+4])
	}

	for j := 16; j < 68; j++ {
		a := w[j-16] ^ w[j-9] ^ leftRotate(w[j-3], 15)
		a = a ^ leftRotate(a, 15) ^ leftRotate(a, 23)
		w[j] = (a ^ leftRotate(w[j-13], 7) ^ w[j-6])
	}

	for j := 0; j < 64; j++ {
		w[j+68] = w[j] ^ w[j+4]
	}

	a, b, c, d, e, f, g, h := sm3.reg[0], sm3.reg[1], sm3.reg[2], sm3.reg[3], sm3.reg[4], sm3.reg[5], sm3.reg[6], sm3.reg[7]

	for j := 0; j < 64; j++ {
		ss1 := leftRotate((leftRotate(a, 12) + e + leftRotate(getTj(j), j)), 7)
		ss2 := ss1 ^ leftRotate(a, 12)
		tt1 := (ffj(j, a, b, c) + d + ss2 + w[j+68])
		tt2 := (ggj(j, e, f, g) + h + ss1 + w[j])

		d = c
		c = leftRotate(b, 9)
		b = a
		a = tt1
		h = g
		g = leftRotate(f, 19)
		f = e
		e = (tt2 ^ leftRotate(tt2, 9) ^ leftRotate(tt2, 17))
	}

	sm3.reg[0] ^= a
	sm3.reg[1] ^= b
	sm3.reg[2] ^= c
	sm3.reg[3] ^= d
	sm3.reg[4] ^= e
	sm3.reg[5] ^= f
	sm3.reg[6] ^= g
	sm3.reg[7] ^= h
}

func (sm3 *SM3) Sum(data []byte) []byte {
	if data != nil {
		sm3.Reset()
		sm3.Write(data)
	}

	sm3.fill()

	for i := 0; i < len(sm3.chunk); i += 64 {
		end := min(i+64, len(sm3.chunk))
		sm3.compress(sm3.chunk[i:end])
	}

	result := make([]byte, 32)
	for i, val := range sm3.reg {
		binary.BigEndian.PutUint32(result[4*i:4*i+4], val)
	}

	sm3.Reset()
	return result
}

// RC4 加密
func rc4Encrypt(plaintext string, key string) string {
	cipher, err := rc4.NewCipher([]byte(key))
	if err != nil {
		// RC4 密钥无效，返回空字符串
		return ""
	}
	dst := make([]byte, len(plaintext))
	cipher.XORKeyStream(dst, []byte(plaintext))
	return string(dst)
}

// 结果加密（魔改Base64）
func resultEncrypt(longStr string, num string) string {
	encodingTables := map[string]string{
		"s0": "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=",
		"s1": "Dkdpgh4ZKsQB80/Mfvw36XI1R25+WUAlEi7NLboqYTOPuzmFjJnryx9HVGcaStCe=",
		"s2": "Dkdpgh4ZKsQB80/Mfvw36XI1R25-WUAlEi7NLboqYTOPuzmFjJnryx9HVGcaStCe=",
		"s3": "ckdp1h4ZKsUB80/Mfvw36XIgR25+WQAlEi7NLboqYTOPuzmFjJnryx9HVGDaStCe",
		"s4": "Dkdpgh2ZmsQB80/MfvV36XI1R45-WUAlEixNLwoqYTOPuzKFjJnry79HbGcaStCe",
	}

	masks := []uint32{16515072, 258048, 4032, 63}
	shifts := []int{18, 12, 6, 0}
	encodingTable := encodingTables[num]

	result := make([]byte, 0)
	roundNum := 0
	longInt := getLongInt(roundNum, longStr)

	totalChars := int(math.Ceil(float64(len(longStr)) / 3 * 4))

	for i := 0; i < totalChars; i++ {
		if i/4 != roundNum {
			roundNum++
			longInt = getLongInt(roundNum, longStr)
		}

		index := i % 4
		charIndex := (longInt & masks[index]) >> shifts[index]
		result = append(result, encodingTable[charIndex])
	}

	return string(result)
}

func getLongInt(roundNum int, longStr string) uint32 {
	roundNum = roundNum * 3
	var char1, char2, char3 byte
	if roundNum < len(longStr) {
		char1 = longStr[roundNum]
	}
	if roundNum+1 < len(longStr) {
		char2 = longStr[roundNum+1]
	}
	if roundNum+2 < len(longStr) {
		char3 = longStr[roundNum+2]
	}
	return uint32(char1)<<16 | uint32(char2)<<8 | uint32(char3)
}

func generRandom(randomNum int, option []int) []byte {
	byte1 := byte(randomNum & 255)
	byte2 := byte((randomNum >> 8) & 255)

	return []byte{
		(byte1 & 170) | (byte(option[0]) & 85),
		(byte1 & 85) | (byte(option[0]) & 170),
		(byte2 & 170) | (byte(option[1]) & 85),
		(byte2 & 85) | (byte(option[1]) & 170),
	}
}

func generateRandomStr() string {
	randomValues := []float64{0.123456789, 0.987654321, 0.555555555}
	randomBytes := make([]byte, 0)

	randomBytes = append(randomBytes, generRandom(int(randomValues[0]*10000), []int{3, 45})...)
	randomBytes = append(randomBytes, generRandom(int(randomValues[1]*10000), []int{1, 0})...)
	randomBytes = append(randomBytes, generRandom(int(randomValues[2]*10000), []int{1, 5})...)

	return string(randomBytes)
}

func splitToBytes(num uint32) []byte {
	return []byte{
		byte(num >> 24),
		byte(num >> 16),
		byte(num >> 8),
		byte(num),
	}
}

// generateRc4BbStr 拼出签名的主体。
//
// startTime 由调用方传入而不是在这里读墙钟：它是被编进签名的字段，
// 留在这里取时间就等于让"同输入同输出"永远不成立 —— 那样回归基准也冻结不了，
// 算法被改坏了也看不出来。能把时间钉住，才谈得上对账。
func generateRc4BbStr(urlSearchParams, userAgent, windowEnvStr string, suffix string, arguments []int, startTime uint32) string {
	sm3 := NewSM3()

	// 计算三次哈希
	urlHash := sm3.Sum(sm3.Sum([]byte(urlSearchParams + suffix)))
	cusHash := sm3.Sum(sm3.Sum([]byte(suffix)))
	uaKey := string([]byte{0, 1, 14})
	uaEncrypted := rc4Encrypt(userAgent, uaKey)
	uaEncoded := resultEncrypt(uaEncrypted, "s3")
	uaHash := sm3.Sum([]byte(uaEncoded))

	endTime := startTime + 100

	b := make(map[int]interface{})
	b[8] = 3
	b[10] = endTime
	b[15] = map[string]interface{}{
		"aid":    6383,
		"pageId": 110624,
		"boe":    false,
		"ddrt":   7,
		"paths": map[string]interface{}{
			"include": make([]map[string]interface{}, 7),
			"exclude": []interface{}{},
		},
		"track": map[string]interface{}{
			"mode":  0,
			"delay": 300,
			"paths": []interface{}{},
		},
		"dump": true,
		"rpU":  "hwj",
	}
	b[16] = startTime
	b[18] = 44
	b[19] = []int{1, 0, 1, 5}

	startTimeBytes := splitToBytes(b[16].(uint32))
	b[20] = startTimeBytes[0]
	b[21] = startTimeBytes[1]
	b[22] = startTimeBytes[2]
	b[23] = startTimeBytes[3]
	b[24] = byte(uint64(b[16].(uint32)) / 256 / 256 / 256 / 256 & 255)
	b[25] = byte(uint64(b[16].(uint32)) / 256 / 256 / 256 / 256 / 256 & 255)

	arg0Bytes := splitToBytes(uint32(arguments[0]))
	b[26] = arg0Bytes[0]
	b[27] = arg0Bytes[1]
	b[28] = arg0Bytes[2]
	b[29] = arg0Bytes[3]

	b[30] = byte(arguments[1] / 256 & 255)
	b[31] = byte(arguments[1] % 256 & 255)

	arg1Bytes := splitToBytes(uint32(arguments[1]))
	b[32] = arg1Bytes[0]
	b[33] = arg1Bytes[1]

	arg2Bytes := splitToBytes(uint32(arguments[2]))
	b[34] = arg2Bytes[0]
	b[35] = arg2Bytes[1]
	b[36] = arg2Bytes[2]
	b[37] = arg2Bytes[3]

	b[38] = urlHash[21]
	b[39] = urlHash[22]
	b[40] = cusHash[21]
	b[41] = cusHash[22]
	b[42] = uaHash[23]
	b[43] = uaHash[24]

	endTimeBytes := splitToBytes(b[10].(uint32))
	b[44] = endTimeBytes[0]
	b[45] = endTimeBytes[1]
	b[46] = endTimeBytes[2]
	b[47] = endTimeBytes[3]
	b[48] = b[8].(int)
	b[49] = byte(uint64(b[10].(uint32)) / 256 / 256 / 256 / 256 & 255)
	b[50] = byte(uint64(b[10].(uint32)) / 256 / 256 / 256 / 256 / 256 & 255)

	b[51] = b[15].(map[string]interface{})["pageId"].(int)
	pageIdBytes := splitToBytes(uint32(b[51].(int)))
	b[52] = pageIdBytes[0]
	b[53] = pageIdBytes[1]
	b[54] = pageIdBytes[2]
	b[55] = pageIdBytes[3]

	b[56] = b[15].(map[string]interface{})["aid"].(int)
	b[57] = byte(b[56].(int) & 255)
	b[58] = byte((b[56].(int) >> 8) & 255)
	b[59] = byte((b[56].(int) >> 16) & 255)
	b[60] = byte((b[56].(int) >> 24) & 255)

	windowEnvList := []byte(windowEnvStr)
	b[64] = len(windowEnvList)
	b[65] = byte(len(windowEnvList) & 255)
	b[66] = byte((len(windowEnvList) >> 8) & 255)

	b[69] = 0
	b[70] = 0
	b[71] = 0

	b[72] = b[18].(int) ^ int(b[20].(byte)) ^ int(b[26].(byte)) ^ int(b[30].(byte)) ^ int(b[38].(byte)) ^ int(b[40].(byte)) ^ int(b[42].(byte)) ^ int(b[21].(byte)) ^ int(b[27].(byte)) ^ int(b[31].(byte)) ^
		int(b[35].(byte)) ^ int(b[39].(byte)) ^ int(b[41].(byte)) ^ int(b[43].(byte)) ^ int(b[22].(byte)) ^ int(b[28].(byte)) ^ int(b[32].(byte)) ^ int(b[36].(byte)) ^ int(b[23].(byte)) ^ int(b[29].(byte)) ^
		int(b[33].(byte)) ^ int(b[37].(byte)) ^ int(b[44].(byte)) ^ int(b[45].(byte)) ^ int(b[46].(byte)) ^ int(b[47].(byte)) ^ int(b[48].(int)) ^ int(b[49].(byte)) ^ int(b[50].(byte)) ^ int(b[24].(byte)) ^
		int(b[25].(byte)) ^ int(b[52].(byte)) ^ int(b[53].(byte)) ^ int(b[54].(byte)) ^ int(b[55].(byte)) ^ int(b[57].(byte)) ^ int(b[58].(byte)) ^ int(b[59].(byte)) ^ int(b[60].(byte)) ^ int(b[65].(byte)) ^
		int(b[66].(byte)) ^ int(b[70].(int)) ^ int(b[71].(int))

	bb := []byte{
		byte(b[18].(int)), b[20].(byte), b[52].(byte), b[26].(byte), b[30].(byte), b[34].(byte), b[58].(byte), b[38].(byte), b[40].(byte), b[53].(byte), b[42].(byte), b[21].(byte),
		b[27].(byte), b[54].(byte), b[55].(byte), b[31].(byte), b[35].(byte), b[57].(byte), b[39].(byte), b[41].(byte), b[43].(byte), b[22].(byte), b[28].(byte), b[32].(byte),
		b[60].(byte), b[36].(byte), b[23].(byte), b[29].(byte), b[33].(byte), b[37].(byte), b[44].(byte), b[45].(byte), b[59].(byte), b[46].(byte), b[47].(byte), byte(b[48].(int)),
		b[49].(byte), b[50].(byte), b[24].(byte), b[25].(byte), b[65].(byte), b[66].(byte), byte(b[70].(int)), byte(b[71].(int)),
	}
	bb = append(bb, windowEnvList...)
	bb = append(bb, byte(b[72].(int)))

	bbStr := string(bb)
	return rc4Encrypt(bbStr, string([]byte{121}))
}

// AbSign 生成抖音 a_bogus 签名。
//
//	urlSearchParams: 参与签名的整串 query（不含 a_bogus 自己，顺序与实发一致）
//	userAgent: 浏览器 User-Agent，必须与接下来真正发出去的那个完全相同
//
// 返回的串结尾那个 "=" 是签名本体的一部分，不能剪（剪了抖音回 200+空体）。
func AbSign(urlSearchParams, userAgent string) string {
	return AbSignWithEnv(urlSearchParams, userAgent, DefaultWindowEnv)
}

// DefaultWindowEnv 是"浏览器窗口环境"那串字面量（屏幕/窗口尺寸/缩放/平台）。
//
// 它不是装饰：**它被编进签名**。实测（2026-10-15，同一房间只改签名里那一段、实发参数不变）：
//
//	Win32 / MacIntel      → 认（拿到完整 JSON）
//	Linux x86_64 / iPhone / MacPPC / Fake → **不认：200 + 0 字节**，不给错误码
//	尺寸全 0 + 假平台          → 不认
//
// 两个可直接执行的结论：
//  1. 平台那一段有**白名单**，不是随便填。这与画像池"Linux 形态被静默拒"是同一件事的两侧：
//     UA 字串与签名内容各查一次，所以光改 UA 不够。
//  2. **与实发参数同不同源并不检查**：参数一直声明 MacIntel、签名换成 Win32 照样认。
//     因此同源是主动选择（少送一个自相矛盾），不是硬性约束 —— 不要把前者当后者的理由。
const DefaultWindowEnv = "1920|1080|1920|1040|0|30|0|0|1872|92|1920|1040|1857|92|1|24|Win32"

// WindowEnv 按真实身份生成那串环境，用在"签名必须与实发参数同源"的地方。
//
// platform 只能是 Win32 或 MacIntel（见 DefaultWindowEnv 上的实测白名单）；
// 其余形态会直接把请求变成 200+空体，且不报错。
//
// 中间那几个数是从默认串的自身算术推出来的（1040=1080-40、1872=1920-48、1857=1920-63），
// **不是官方字段语义** —— 实测它们怎么变都不影响成败，只保平台段不能乱。
func WindowEnv(platform string, screenWidth, screenHeight int) string {
	return fmt.Sprintf("%d|%d|%d|%d|0|30|0|0|%d|92|%d|%d|%d|92|1|24|%s",
		screenWidth, screenHeight, screenWidth, screenHeight-40,
		screenWidth-48, screenWidth, screenHeight-40, screenWidth-63, platform)
}

// AbSignWithEnv 指定窗口环境串签名（其余与 AbSign 完全一致）。
func AbSignWithEnv(urlSearchParams, userAgent, windowEnvStr string) string {
	return abSignAt(urlSearchParams, userAgent, windowEnvStr, uint32(time.Now().UnixMilli()))
}

// abSignAt 把时间与窗口环境都固定下来：同一组输入必得同一个串。
// 这是仓内第一份能冻结的签名基准的前提（参考项目也是先把随机量钉死才做对账）。
func abSignAt(urlSearchParams, userAgent, windowEnvStr string, startTime uint32) string {
	return resultEncrypt(
		generateRandomStr()+
			generateRc4BbStr(urlSearchParams, userAgent, windowEnvStr, "cus", []int{0, 1, 14}, startTime),
		"s4",
	) + "="
}
