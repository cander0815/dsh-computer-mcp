"""补齐旧工具链的 Mach-O UUID，并标注承诺支持的最低系统版本。"""
import hashlib
from pathlib import Path
import struct
import sys

path = Path(sys.argv[1])
data = bytearray(path.read_bytes())
assert struct.unpack_from('<I', data)[0] == 0xFEEDFACF
cpu = struct.unpack_from('<I', data, 4)[0]
minimum = 0x000A0F00 if cpu == 0x01000007 else 0x000B0000
count, total = struct.unpack_from('<II', data, 16)
pos = 32
has_uuid = False
section_offsets = []
for _ in range(count):
    cmd, size = struct.unpack_from('<II', data, pos)
    assert size >= 8
    if cmd == 0x1B:
        has_uuid = True
    if cmd == 0x32:
        struct.pack_into('<I', data, pos + 12, minimum)
    if cmd == 0x24:
        struct.pack_into('<I', data, pos + 8, minimum)
    if cmd == 0x19:
        nsects = struct.unpack_from('<I', data, pos + 64)[0]
        for i in range(nsects):
            offset = struct.unpack_from('<I', data, pos + 72 + i * 80 + 48)[0]
            if offset:
                section_offsets.append(offset)
    pos += size
assert pos == 32 + total
if not has_uuid:
    assert pos + 24 <= min(section_offsets)
    assert data[pos:pos + 24] == bytes(24)
    uuid = hashlib.sha256(data).digest()[:16]
    data[pos:pos + 24] = struct.pack('<II', 0x1B, 24) + uuid
    struct.pack_into('<II', data, 16, count + 1, total + 24)
path.write_bytes(data)
print(path.name, 'Mach-O UUID 和最低系统版本已核对')
