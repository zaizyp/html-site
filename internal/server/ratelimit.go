// ratelimit.go：登录失败计数 + 临时锁定,防暴力枚举密码。
//
// 设计:纯内存计数器(进程内有效,重启清零),按 用户名|IP 维度统计连续失败次数。
//   - 累计失败达 maxFails 次 → 锁定 lockWindow 时长,期间该 key 的所有登录尝试直接拒绝
//   - 登录成功立即清零
//   - 锁定窗口过后下次访问惰性自动解锁
//   - map 超过 pruneAt 条时顺手清理已过期项,防内存无限增长
//
// 适用场景:内网单实例部署。多实例需换共享存储(Redis 等),本实现不覆盖。
package server

import (
	"sync"
	"time"
)

const (
	loginMaxFails   = 5                // 连续失败阈值
	loginLockWindow = 15 * time.Minute // 锁定时长
	loginPruneAt    = 10_000           // 表条数上限,超过则清理过期项
)

// loginLimiter 登录失败计数器(线程安全)。
type loginLimiter struct {
	mu  sync.Mutex
	rec map[string]*loginRec
}

type loginRec struct {
	fails    int       // 连续失败次数
	lastFail time.Time // 最近一次失败时间(用于惰性过期判断)
}

// newLoginLimiter 构造一个登录限流器。
func newLoginLimiter() *loginLimiter {
	return &loginLimiter{rec: make(map[string]*loginRec)}
}

// allow 返回该 key 当前是否允许尝试登录。
// 若仍处于锁定窗口内返回 false(并附带剩余锁定时长,便于提示)。
func (l *loginLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	r, ok := l.rec[key]
	if !ok || r.fails < loginMaxFails {
		return true, 0
	}
	// 已达阈值:判断是否仍在锁定窗口内
	if time.Since(r.lastFail) < loginLockWindow {
		return false, loginLockWindow - time.Since(r.lastFail)
	}
	// 锁定窗口已过,重置计数
	delete(l.rec, key)
	return true, 0
}

// fail 记录一次失败。惰性清理过期项防内存膨胀。
func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rec[key] == nil {
		l.rec[key] = &loginRec{}
	}
	l.rec[key].fails++
	l.rec[key].lastFail = time.Now()

	// 表过大时顺手清理:锁定窗口外的旧记录可安全移除
	if len(l.rec) > loginPruneAt {
		now := time.Now()
		for k, v := range l.rec {
			if v.fails < loginMaxFails || now.Sub(v.lastFail) >= loginLockWindow {
				delete(l.rec, k)
			}
		}
	}
}

// success 清除某 key 的失败计数(登录成功后调用)。
func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.rec, key)
}
