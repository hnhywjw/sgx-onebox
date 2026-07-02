package runtimebundle

import (
	"log"
	"os"
	"path/filepath"
)

func ResolveBuiltInBundleDir() string {
	if dir := os.Getenv("PLATFORM_RUNTIME_BUNDLES_DIR"); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			return filepath.Join(filepath.Dir(resolved), "runtime-bundles")
		}
		return filepath.Join(filepath.Dir(exe), "runtime-bundles")
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("获取工作目录失败: %v，回退到当前目录", err)
		return "runtime-bundles"
	}
	return filepath.Join(cwd, "runtime-bundles")
}
