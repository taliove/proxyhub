package main

import "fmt"

// 版本信息由构建注入(-ldflags "-X main.version=... -X main.buildTime=...")。
// 本地 make build 恒为 dev;release 制品由 scripts/release/package.sh 注入
// VERSION 文件值与 SOURCE_DATE_EPOCH 对齐的构建时间(可重现构建)。
var (
	version   = "dev"
	buildTime = ""
)

// versionString 返回单行版本信息,供 version 子命令打印。
func versionString() string {
	if buildTime == "" {
		return fmt.Sprintf("proxyhub %s", version)
	}
	return fmt.Sprintf("proxyhub %s (built %s)", version, buildTime)
}
