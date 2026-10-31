package cmd

import (
	"fmt"
	"sync/atomic"
)

var testServerSeq atomic.Int64

// uniqueServerName 为每次测试生成唯一的 g.Server 名称。
// g.Server(name) 是按名称缓存的单例，重复使用同名实例会在 go test -count>1 时触发 "duplicated route registry"。
func uniqueServerName(name string) string {
	return fmt.Sprintf("%s-%d", name, testServerSeq.Add(1))
}
