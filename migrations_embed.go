// Package adcenter 仅承载 go:embed 的迁移文件，供 internal/migrate 在构建期烘焙进
// 二进制。运行镜像（见 Dockerfile）只拷贝二进制、不含 migrations/ 目录，因此迁移必须
// 以 embed 形式随二进制发布，而不能在运行时从文件系统读取。
package adcenter

import "embed"

// MigrationFS 包含 migrations/ 下全部 .up.sql（DOWN 文件不参与自动迁移，故不嵌入）。
//
//go:embed migrations/*.up.sql
var MigrationFS embed.FS
