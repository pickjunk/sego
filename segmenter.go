package sego

import (
	"bufio"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minTokenFrequency = 2 // 仅从字典文件中读取大于等于此频率的分词
)

const (
	wordAlpha  = iota
	wordNumber = iota
	wordOther  = iota
)

// Segmenter 分词器结构体
type Segmenter struct {
	dict *Dictionary
}

// 该结构体用于记录Viterbi算法中某字元处的向前分词跳转信息
type jumper struct {
	minDistance float32
	token       *Token
}

// Dictionary 返回分词器使用的词典
func (seg *Segmenter) Dictionary() *Dictionary {
	return seg.dict
}

// LoadDictionary 从文件中载入词典
//
// 可以载入多个词典文件，文件名用","分隔，排在前面的词典优先载入分词，比如
// 	"用户词典.txt,通用词典.txt"
// 当一个分词既出现在用户词典也出现在通用词典中，则优先使用用户词典。
//
// 词典的格式为（每个分词一行）：
//	分词文本 频率 词性
func (seg *Segmenter) LoadDictionary(files string) {
	seg.dict = NewDictionary()
	for _, file := range strings.Split(files, ",") {
		log.Info().Str("file", file).Msg("载入词典")
		dictFile, err := os.Open(file)
		defer dictFile.Close()
		if err != nil {
			log.Fatal().Str("file", file).Msg("无法载入词典文件")
		}

		reader := bufio.NewReader(dictFile)
		var text string
		var freqText string
		var frequency int
		var pos string

		// 逐行读入分词
		for {
			line, eof := reader.ReadString('\n')
			if eof == nil {
				// 清除末尾的'\n'
				line = line[:len(line)-1]
			}

			pieces := strings.Split(strings.Trim(line, " "), "|")
			var synonyms []*Token
			for _, piece := range pieces {
				slices := strings.Split(strings.Trim(piece, " "), " ")
				l := len(slices)

				// 最后一个元素为数字（词频）
				if regexp.MustCompile("^\\d+$").MatchString(slices[l-1]) {
					// 格式：[词] [词频]，至少要有两个元素
					if l < 2 {
						break
					}

					text = strings.Join(slices[:l-1], " ")
					freqText = slices[l-1]
					pos = ""
				} else {
					// 格式：[词] [词频] [词性]，至少要有三个元素
					if l < 3 {
						break
					}

					text = strings.Join(slices[:l-2], " ")
					// 特殊符号转义
					text = strings.Replace(text, "__VERTICAL_BAR__", "|", -1)
					freqText = slices[l-2]
					pos = slices[l-1]
				}

				// 词为空，无效行
				if text == "" {
					break
				}

				// 解析词频
				var err error
				frequency, err = strconv.Atoi(freqText)
				if err != nil {
					continue
				}

				// 过滤频率太小的词
				if frequency < minTokenFrequency {
					continue
				}

				words := splitTextToWords([]byte(text))
				token := Token{text: words, frequency: frequency, pos: pos}

				// 添加到同义词数组
				synonyms = append(synonyms, &token)
			}

			for i, token := range synonyms {
				token.synonyms = append(token.synonyms, synonyms[:i]...)
				token.synonyms = append(token.synonyms, synonyms[i+1:]...)
				// log.Info().Str("synonyms", token.SynonymsText()).Send()

				// 将分词添加到字典中
				seg.dict.addToken(token)
			}

			// 文件结束
			if eof != nil {
				break
			}
		}
	}

	// 计算每个分词的路径值，路径值含义见Token结构体的注释
	logTotalFrequency := float32(math.Log2(float64(seg.dict.totalFrequency)))
	for i := range seg.dict.tokens {
		token := seg.dict.tokens[i]
		token.distance = logTotalFrequency - float32(math.Log2(float64(token.frequency)))
	}

	// 对每个分词进行细致划分，用于搜索引擎模式，该模式用法见Token结构体的注释。
	for i := range seg.dict.tokens {
		token := seg.dict.tokens[i]

		// 子分词
		segments := seg.segmentWords(token.text, true)
		for i := 0; i < len(segments); i++ {
			token.segments = append(token.segments, &segments[i])
		}

		// 找出所有子分词的同义词，按笛卡尔积算出该词的所有同义词
		synonyms := []*Token{
			{
				frequency: token.frequency,
				distance:  token.distance,
				pos:       token.pos,
			},
		}
		hasSynonyms := false
		for _, segment := range token.segments {
			var cartesian []*Token

			for _, a := range synonyms {
				if len(segment.token.synonyms) > 0 {
					hasSynonyms = true
					for _, b := range segment.token.synonyms {
						var text []Text
						text = append(text, a.text...)
						text = append(text, b.text...)
						cartesian = append(cartesian, &Token{
							text:      text,
							frequency: a.frequency,
							distance:  a.distance,
							pos:       a.pos,
						})
					}
				} else {
					var text []Text
					text = append(text, a.text...)
					text = append(text, segment.token.text...)
					cartesian = append(cartesian, &Token{
						text:      text,
						frequency: a.frequency,
						distance:  a.distance,
						pos:       a.pos,
					})
				}
			}

			synonyms = cartesian
		}

		if hasSynonyms {
			token.synonyms = synonyms

			for i, t := range token.synonyms {
				// 子分词
				segments := seg.segmentWords(t.text, true)
				for i := 0; i < len(segments); i++ {
					t.segments = append(t.segments, &segments[i])
				}

				// 同义词的同义词
				t.synonyms = append(t.synonyms, token)
				t.synonyms = append(t.synonyms, synonyms[:i]...)
				t.synonyms = append(t.synonyms, synonyms[i+1:]...)

				// 添加同义词到词库
				seg.dict.addToken(t)
			}
		}
	}

	log.Info().Msg("词典载入完毕")
}

// Segment 对文本分词
//
// 输入参数：
//	bytes	UTF8文本的字节数组
//
// 输出：
//	[]Segment	划分的分词
func (seg *Segmenter) Segment(bytes []byte) []Segment {
	return seg.internalSegment(bytes, false)
}

// FullSegment 对文本进行全分词
//
// 输入参数：
//	bytes	UTF8文本的字节数组
//
// 输出：
//	[]Segment	划分的分词
func (seg *Segmenter) FullSegment(bytes []byte) []Segment {
	segments := seg.internalSegment(bytes, false)

	// 分词扩展，扩展出子分词、同义词
	segments = SegmentsSpread(segments)

	return segments
}

// InternalSegment 对文本分词
func (seg *Segmenter) InternalSegment(bytes []byte, searchMode bool) []Segment {
	return seg.internalSegment(bytes, searchMode)
}

func (seg *Segmenter) internalSegment(bytes []byte, searchMode bool) []Segment {
	// 处理特殊情况
	if len(bytes) == 0 {
		return []Segment{}
	}

	// 划分字元
	text := splitTextToWords(bytes)

	return seg.segmentWords(text, searchMode)
}

func (seg *Segmenter) segmentWords(text []Text, searchMode bool) []Segment {
	// 搜索模式下该分词已无继续划分可能的情况
	if searchMode && len(text) == 1 {
		return []Segment{}
	}

	// jumpers定义了每个字元处的向前跳转信息，包括这个跳转对应的分词，
	// 以及从文本段开始到该字元的最短路径值
	jumpers := make([]jumper, len(text))

	tokens := make([]*Token, seg.dict.maxTokenLength)
	for current := 0; current < len(text); current++ {
		// 找到前一个字元处的最短路径，以便计算后续路径值
		var baseDistance float32
		if current == 0 {
			// 当本字元在文本首部时，基础距离应该是零
			baseDistance = 0
		} else {
			baseDistance = jumpers[current-1].minDistance
		}

		// 寻找所有以当前字元开头的分词
		numTokens := seg.dict.lookupTokens(
			text[current:minInt(current+seg.dict.maxTokenLength, len(text))], tokens)

		// 对所有可能的分词，更新分词结束字元处的跳转信息
		for iToken := 0; iToken < numTokens; iToken++ {
			location := current + len(tokens[iToken].text) - 1
			if !searchMode || current != 0 || location != len(text)-1 {
				updateJumper(&jumpers[location], baseDistance, tokens[iToken])
			}
		}

		// 当前字元没有对应分词时补加一个伪分词
		if numTokens == 0 || len(tokens[0].text) > 1 {
			updateJumper(&jumpers[current], baseDistance,
				&Token{text: []Text{text[current]}, frequency: 1, distance: 32, pos: "x"})
		}
	}

	// 从后向前扫描第一遍得到需要添加的分词数目
	numSeg := 0
	for index := len(text) - 1; index >= 0; {
		location := index - len(jumpers[index].token.text) + 1
		numSeg++
		index = location - 1
	}

	// 从后向前扫描第二遍添加分词到最终结果
	outputSegments := make([]Segment, numSeg)
	for index := len(text) - 1; index >= 0; {
		location := index - len(jumpers[index].token.text) + 1
		numSeg--
		outputSegments[numSeg].token = jumpers[index].token
		index = location - 1
	}

	// 计算各个分词的字节位置
	bytePosition := 0
	for iSeg := 0; iSeg < len(outputSegments); iSeg++ {
		outputSegments[iSeg].start = bytePosition
		bytePosition += textSliceByteLength(outputSegments[iSeg].token.text)
		outputSegments[iSeg].end = bytePosition
	}

	// 过滤停止词
	var resultSegments []Segment
	for _, segment := range outputSegments {
		if segment.Token().Pos() != "__STOP__" {
			resultSegments = append(resultSegments, segment)
		}
	}

	return resultSegments
}

// 更新跳转信息:
// 	1. 当该位置从未被访问过时(jumper.minDistance为零的情况)，或者
//	2. 当该位置的当前最短路径大于新的最短路径时
// 将当前位置的最短路径值更新为baseDistance加上新分词的概率
func updateJumper(jumper *jumper, baseDistance float32, token *Token) {
	newDistance := baseDistance + token.distance
	if jumper.minDistance == 0 || jumper.minDistance > newDistance {
		jumper.minDistance = newDistance
		jumper.token = token
	}
}

// 取两整数较小值
func minInt(a, b int) int {
	if a > b {
		return b
	}
	return a
}

// 取两整数较大值
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// 将文本划分成字元
func splitTextToWords(text Text) []Text {
	output := make([]Text, 0, len(text)/3)
	current := 0
	preWordType := wordAlpha
	preWordStart := 0
	for current < len(text) {
		r, size := utf8.DecodeRune(text[current:])

		curWordType := wordOther
		switch {
		case size <= 2 && unicode.IsLetter(r):
			curWordType = wordAlpha
		case size <= 2 && unicode.IsNumber(r):
			curWordType = wordNumber
		}

		if curWordType != preWordType || curWordType == wordOther {
			if current != 0 {
				word := text[preWordStart:current]
				if preWordType == wordAlpha {
					word = toLower(word)
				}
				if string(word) != " " {
					output = append(output, word)
				}
			}

			preWordType = curWordType
			preWordStart = current
		}

		current += size
	}

	// 边界情况
	if current != 0 {
		word := text[preWordStart:current]
		if preWordType == wordAlpha {
			word = toLower(word)
		}
		if string(word) != " " {
			output = append(output, word)
		}
	}

	return output
}

// 将英文词转化为小写
func toLower(text []byte) []byte {
	output := make([]byte, len(text))
	for i, t := range text {
		if t >= 'A' && t <= 'Z' {
			output[i] = t - 'A' + 'a'
		} else {
			output[i] = t
		}
	}
	return output
}
