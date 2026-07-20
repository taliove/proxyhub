package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/store"
)

// state-fingerprint 子命令: 输出留存数据的 HMAC 认证指纹,用于证明升级没有
// 静默丢失/改写保留数据(见 production-release ticket 03)。
//
// 输出格式(稳定、机器可解析,每行一个 key=value):
//
//	fingerprint_version=2
//	schema_hash=<64 hex>                       核心表存在性散列(与认证密钥无关)
//	settings_count=<n>
//	settings_hash=<64 hex>
//	settings_record_<idhash>=<64 hex>          每条记录一行,同集合内按 idhash 升序
//	endpoints_count=...                        其余集合同构: endpoints, airports,
//	...                                        self_hosted_nodes, distribution_paths,
//	                                           distribution_nodes
//
// 用法(升级前后各执行一次,使用同一密钥,再比对两份输出):
//
//	echo '<至少 32 字节的一次性密钥>' | proxyhub state-fingerprint --config config.yaml --authentication-key-stdin
//
// 密钥只从标准输入读取,不出现在命令行参数与 shell 历史中;不写入任何文件。
func runStateFingerprint(args []string) error {
	fs := flag.NewFlagSet("state-fingerprint", flag.ContinueOnError)
	configPath := fs.String("config", "config.yaml", "配置文件路径")
	keyFromStdin := fs.Bool("authentication-key-stdin", false, "从标准输入读取一行认证密钥(必需)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*keyFromStdin {
		return errors.New("缺少 --authentication-key-stdin: 认证密钥只能从标准输入读取,避免进入 shell 历史")
	}
	key, err := readAuthenticationKey(os.Stdin)
	if err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}
	st, err := store.Open(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer st.Close()

	fp, err := st.RetainedStateFingerprint(key)
	if err != nil {
		return fmt.Errorf("计算指纹: %w", err)
	}

	out := bufio.NewWriter(os.Stdout)
	for _, line := range fp.Lines() {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return fmt.Errorf("输出指纹: %w", err)
		}
	}
	return out.Flush()
}

// readAuthenticationKey 从 r 读取一行作为认证密钥。
// 只剥离行尾换行符(密钥本身允许含空格);长度至少 32 字节(HMAC 密钥材料按字节计)。
func readAuthenticationKey(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("读取认证密钥: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, errors.New("认证密钥为空: 请通过标准输入传入一行至少 32 字节的密钥")
	}
	key := []byte(line)
	if len(key) < 32 {
		return nil, fmt.Errorf("认证密钥过短: %d 字节,至少需要 32 字节", len(key))
	}
	return key, nil
}
