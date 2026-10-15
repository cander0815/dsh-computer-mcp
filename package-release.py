"""打包跨平台发布文件，不包含配置、令牌和每日操作日志。"""
from pathlib import Path
import hashlib
import re
import zipfile

base = Path(__file__).resolve().parent
version = re.search(r'const version = "([^"]+)"', (base / 'config.go').read_text()).group(1)
release = base / 'releases'
release.mkdir(exist_ok=True)
normal = '''#!/bin/sh
# 普通用户启动，保持当前终端窗口打开。
cd "$(dirname "$0")" || exit 1
exec ./dsh-computer-mcp -log-dir "$PWD"
'''
sudo = '''#!/bin/sh
# 先以原用户准备配置，管理员进程继续使用相同配置和令牌。
set -eu
cd "$(dirname "$0")"
config="$HOME/Library/Application Support/DSHComputerAgent/config.json"
./dsh-computer-mcp -prepare-config -config "$config"
printf '%s\\n' '管理员模式会扩大命令权限。请先关闭普通连接器，然后输入本机系统密码。'
exec sudo ./dsh-computer-mcp -config "$config" -log-dir "$PWD"
'''
windows = '''@echo off\r
chcp 65001 >nul\r
cd /d "%~dp0"\r
dsh-computer-mcp.exe -log-dir "%~dp0."\r
pause\r
'''
paths = []
for platform in ['windows-amd64', 'macos-amd64', 'macos-arm64', 'linux-amd64']:
    suffix = '.exe' if platform.startswith('windows') else ''
    path = release / f'dsh-computer-mcp-v{version}-{platform}.zip'
    with zipfile.ZipFile(path, 'w', zipfile.ZIP_DEFLATED) as archive:
        archive.write(base / 'dist' / ('dsh-computer-mcp-' + platform + suffix), 'dsh-computer-mcp' + suffix)
        archive.write(base / 'README.md', '使用说明.md')
        archive.write(base / 'VERIFICATION.md', '测试记录.md')
        archive.write(base / 'LICENSE', 'LICENSE')
        launchers = {'启动.cmd': windows} if suffix else {'启动.command': normal}
        if platform.startswith('macos'):
            launchers['启动-sudo.command'] = sudo
        for name, text in launchers.items():
            info = zipfile.ZipInfo(name)
            info.create_system = 3
            info.external_attr = 0o100755 << 16
            archive.writestr(info, text)
    paths.append(path)
source = release / f'dsh-computer-mcp-v{version}-source.zip'
with zipfile.ZipFile(source, 'w', zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(base.iterdir()):
        if path.is_file() and (path.suffix in ['.go', '.cs', '.mod', '.md', '.html', '.sh', '.py', '.mjs', '.cjs'] or path.name in ['.gitignore', 'package.json', 'package-lock.json', 'cordis.patch.yml', 'LICENSE']):
            archive.write(path, 'dsh-computer-mcp/' + path.name)
    for path in sorted((base / 'docs').rglob('*.md')):
        archive.write(path, 'dsh-computer-mcp/' + str(path.relative_to(base)))
    for path in sorted((base / 'tests').rglob('*.mjs')):
        archive.write(path, 'dsh-computer-mcp/' + str(path.relative_to(base)))
paths.append(source)
checks = []
for path in paths:
    with zipfile.ZipFile(path) as archive:
        assert archive.testzip() is None
    checks.append(hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + path.name)
(release / f'SHA256SUMS-v{version}.txt').write_text('\n'.join(checks) + '\n')
print(f'v{version} 的 5 个 ZIP 已生成并校验')
