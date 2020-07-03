package sego

import (
	"bytes"
	"fmt"
	"unicode"
	"unicode/utf8"
)

// SegmentsToString 输出分词结果为字符串
//
// 有两种输出模式，以"中华人民共和国"为例
//
//  普通模式（searchMode=false）输出一个分词"中华人民共和国/ns "
//  搜索模式（searchMode=true） 输出普通模式的再细致切分：
//      "中华/nz 人民/n 共和/nz 共和国/ns 人民共和国/nt 中华人民共和国/ns "
//
// 搜索模式主要用于给搜索引擎提供尽可能多的关键字，详情请见Token结构体的注释。
func SegmentsToString(segs []Segment) (output string) {
	for _, seg := range segs {
		output += fmt.Sprintf(
			"%s/%s ", textSliceToString(seg.token.text), seg.token.pos)
	}
	return
}

func tokenToString(token *Token) (output string) {
	for _, s := range token.segments {
		if s != nil {
			output += tokenToString(s.token)
		}
	}
	output += fmt.Sprintf("%s/%s ", textSliceToString(token.text), token.pos)
	return
}

// SegmentsToSlice 输出分词结果到一个字符串slice
//
// 有两种输出模式，以"中华人民共和国"为例
//
//  普通模式（searchMode=false）输出一个分词"[中华人民共和国]"
//  搜索模式（searchMode=true） 输出普通模式的再细致切分：
//      "[中华 人民 共和 共和国 人民共和国 中华人民共和国]"
//
// 搜索模式主要用于给搜索引擎提供尽可能多的关键字，详情请见Token结构体的注释。
func SegmentsToSlice(segs []Segment) (output []string) {
	for _, seg := range segs {
		output = append(output, seg.token.Text())
	}
	return
}

func tokenToSlice(token *Token) (output []string) {
	for _, s := range token.segments {
		output = append(output, tokenToSlice(s.token)...)
	}
	output = append(output, textSliceToString(token.text))
	return output
}

// SegmentsSpread 分词扩展，从一组分词中，扩展出全部子分词，同义词，以及同义词的子分词
func SegmentsSpread(segs []Segment) (output []Segment) {
	for _, s := range segs {
		// 子分词
		var sub []Segment
		for _, ss := range s.token.segments {
			sub = append(sub, *ss)
		}
		output = append(output, SegmentsSpread(sub)...)

		// 同义词
		for _, t := range s.token.synonyms {
			output = append(output, Segment{
				start: s.start,
				end:   s.end,
				token: t,
			})
		}

		output = append(output, s)
	}
	return
}

// 将多个字元拼接一个字符串输出
func textSliceToString(text []Text) string {
	return Join(text)
}

// Join 把字元slice拼接为字符串
func Join(a []Text) string {
	b := ""

	for i, text := range a {
		b += string(text)

		r, size := utf8.DecodeRune(text)
		if i != len(a)-1 && size <= 2 && (unicode.IsLetter(r) || unicode.IsNumber(r)) {
			b += " "
		}
	}

	return b
}

// 返回多个字元的字节总长度
func textSliceByteLength(text []Text) (length int) {
	for _, word := range text {
		length += len(word)
	}
	return
}

func textSliceToBytes(text []Text) []byte {
	var buf bytes.Buffer
	for _, word := range text {
		buf.Write(word)
	}
	return buf.Bytes()
}
