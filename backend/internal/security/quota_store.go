package security

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

// QuotaStore 是每日额度的持久化后端。key 形如 "<主体哈希>:<YYYY-MM-DD>"，不包含原始 IP 或 Token。
type QuotaStore interface {
	// Incr 在未达到 limit 时将计数加一并返回 true；已达上限返回 false。
	Incr(key string, limit int) (bool, error)
	// Decr 计数减一（不低于 0）。
	Decr(key string) error
	// Get 读取计数。
	Get(key string) (int, error)
	// Cleanup 删除日期不在 keepDates 中的条目。
	Cleanup(keepDates ...string) error
	// Ping 检查存储是否可用（用于 /readyz）。
	Ping() error
	// Close 释放资源。
	Close() error
}

const (
	quotaDBFile = "quota.db"
	// DefaultStateLockTimeout 等待数据库文件锁的最长时间。蓝绿切换时新旧两个容器可能短暂共享同一文件，
	// 每次操作都单独打开/关闭数据库，锁只在单次事务期间持有。
	DefaultStateLockTimeout = 2 * time.Second
)

var quotaBucket = []byte("daily_quota")

// BoltQuotaStore 基于 bbolt 的单文件持久化实现。
type BoltQuotaStore struct {
	path        string
	lockTimeout time.Duration
}

// NewBoltQuotaStore 在 dir 下创建/打开 quota.db。目录不存在时以 0700 创建。
// 文件损坏时将其重命名为 quota.db.corrupt-<时间戳> 后重建（额度从 0 开始，比服务不可用更可接受）。
func NewBoltQuotaStore(dir string, lockTimeout time.Duration) (*BoltQuotaStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("security.stateDir 为空")
	}
	if lockTimeout <= 0 {
		lockTimeout = DefaultStateLockTimeout
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建状态目录失败: %w", err)
	}
	st := &BoltQuotaStore{path: filepath.Join(dir, quotaDBFile), lockTimeout: lockTimeout}
	if err := st.Ping(); err != nil {
		return nil, err
	}
	return st, nil
}

// Path 返回数据库文件路径（测试用）。
func (s *BoltQuotaStore) Path() string { return s.path }

func isCorruptErr(err error) bool {
	return errors.Is(err, bolterrors.ErrInvalid) || errors.Is(err, bolterrors.ErrChecksum) ||
		errors.Is(err, bolterrors.ErrVersionMismatch) || strings.Contains(err.Error(), "invalid database") ||
		strings.Contains(err.Error(), "file size too small")
}

func (s *BoltQuotaStore) open() (*bolt.DB, error) {
	db, err := bolt.Open(s.path, 0o600, &bolt.Options{Timeout: s.lockTimeout})
	if err == nil {
		return db, nil
	}
	if errors.Is(err, bolterrors.ErrTimeout) || !isCorruptErr(err) {
		return nil, fmt.Errorf("打开额度数据库失败: %w", err)
	}
	backup := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().UTC().Format("20060102T150405Z"))
	if rerr := os.Rename(s.path, backup); rerr != nil {
		return nil, fmt.Errorf("额度数据库损坏且无法移走: %w", rerr)
	}
	db, err = bolt.Open(s.path, 0o600, &bolt.Options{Timeout: s.lockTimeout})
	if err != nil {
		return nil, fmt.Errorf("重建额度数据库失败: %w", err)
	}
	return db, nil
}

func (s *BoltQuotaStore) update(fn func(b *bolt.Bucket) error) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()
	return db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(quotaBucket)
		if err != nil {
			return err
		}
		return fn(b)
	})
}

func getCount(b *bolt.Bucket, key string) int {
	v := b.Get([]byte(key))
	if len(v) != 8 {
		return 0
	}
	return int(binary.BigEndian.Uint64(v))
}

func putCount(b *bolt.Bucket, key string, n int) error {
	if n <= 0 {
		return b.Delete([]byte(key))
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(n))
	return b.Put([]byte(key), buf[:])
}

func (s *BoltQuotaStore) Incr(key string, limit int) (bool, error) {
	ok := false
	err := s.update(func(b *bolt.Bucket) error {
		n := getCount(b, key)
		if n >= limit {
			return nil
		}
		ok = true
		return putCount(b, key, n+1)
	})
	return ok, err
}

func (s *BoltQuotaStore) Decr(key string) error {
	return s.update(func(b *bolt.Bucket) error {
		return putCount(b, key, getCount(b, key)-1)
	})
}

func (s *BoltQuotaStore) Get(key string) (int, error) {
	n := 0
	err := s.update(func(b *bolt.Bucket) error {
		n = getCount(b, key)
		return nil
	})
	return n, err
}

func (s *BoltQuotaStore) Cleanup(keepDates ...string) error {
	return s.update(func(b *bolt.Bucket) error {
		var stale [][]byte
		_ = b.ForEach(func(k, _ []byte) error {
			keep := false
			for _, d := range keepDates {
				if strings.HasSuffix(string(k), ":"+d) {
					keep = true
					break
				}
			}
			if !keep {
				stale = append(stale, append([]byte(nil), k...))
			}
			return nil
		})
		for _, k := range stale {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BoltQuotaStore) Ping() error {
	return s.update(func(*bolt.Bucket) error { return nil })
}

// Close 每次操作都会关闭数据库，这里无需额外动作；保留以满足接口与停机流程。
func (s *BoltQuotaStore) Close() error { return nil }

// quotaStoreKey 对主体做 SHA-256，落盘数据不含原始 IP 或 Token 哈希前缀以外的可识别信息。
func quotaStoreKey(principalID, date string) string {
	sum := sha256.Sum256([]byte(principalID))
	return hex.EncodeToString(sum[:16]) + ":" + date
}
