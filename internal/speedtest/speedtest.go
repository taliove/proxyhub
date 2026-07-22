// Package speedtest 本机实测(浏览器端测速)的服务端收发流原语。
// 这是入站服务:浏览器主动打向管理面,与 detection 域的出站检测链路无关,
// 直连出口(Direct Egress)不适用。
package speedtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"time"
)

const (
	// DefaultDownloadDuration 下行发流默认时长(浏览器不传 duration_ms 时)。
	DefaultDownloadDuration = 10 * time.Second
	// MinDownloadDuration 下行发流最短时长,更短测不出稳定速率。
	MinDownloadDuration = time.Second
	// MaxDownloadDuration 下行发流最长时长:全局 WriteTimeout 30s(main.go)内
	// 必须结束,留 5s 余量给连接建立与收尾。
	MaxDownloadDuration = 25 * time.Second
	// DownloadBlockSize 单次写入的随机块大小:远大于 DEFLATE 32KB 滑窗,
	// 循环写这一块也不会产生可压缩的重复匹配。
	DownloadBlockSize = 256 * 1024
	// DownloadMaxBytes 下行发流字节兜底(1 GiB),防极端快链路下时长失效。
	DownloadMaxBytes = 1 << 30
	// MaxUploadBytes 上行收流字节兜底(512 MiB):客户端超时未停流时服务端主动截断。
	MaxUploadBytes = 512 << 20
)

// NewRandomBlock 生成一块不可压缩随机字节(crypto/rand)。
// 下行发流循环写这一块即可:块长远大于 gzip 32KB 滑窗,循环重复不产生
// 可匹配前缀,压缩无法缩水(反例:递增序列带宽数字会虚高,见 spec 补遗 2)。
func NewRandomBlock(size int) ([]byte, error) {
	block := make([]byte, size)
	if _, err := rand.Read(block); err != nil {
		return nil, fmt.Errorf("fill random block: %w", err)
	}
	return block, nil
}

// StreamDownload 向 w 持续写随机块,到 deadline 或 maxBytes 即停,返回已写字节数。
// 每写一块检查一次 ctx(客户端断开即收手);w 若实现 http.Flusher 则每块后
// flush,保证浏览器端按真实到达节奏测速,而非攒在服务端缓冲里。
func StreamDownload(ctx context.Context, w io.Writer, block []byte, deadline time.Time, maxBytes int64) (int64, error) {
	if len(block) == 0 {
		return 0, fmt.Errorf("empty block")
	}
	flusher, canFlush := w.(interface{ Flush() })

	var total int64
	for total < maxBytes && time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		chunk := block
		if rem := maxBytes - total; rem < int64(len(chunk)) {
			chunk = chunk[:rem]
		}
		n, err := w.Write(chunk)
		total += int64(n)
		if canFlush {
			flusher.Flush()
		}
		if err != nil {
			return total, fmt.Errorf("stream download write: %w", err)
		}
	}
	return total, nil
}

// CountUpload 读 r 并丢弃计数,到 maxBytes 截断在上限处(不报错),返回读取字节数。
// 上行时长由客户端控制(到点停止发送即 EOF);maxBytes 是客户端未停流时的服务端兜底。
// 不设 ContentLength 的 chunked 上传与已知长度上传同样适用。
func CountUpload(r io.Reader, maxBytes int64) (int64, error) {
	n, err := io.Copy(io.Discard, io.LimitReader(r, maxBytes))
	if err != nil {
		return n, fmt.Errorf("count upload: %w", err)
	}
	return n, nil
}
