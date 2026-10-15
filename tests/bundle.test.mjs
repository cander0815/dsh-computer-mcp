import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import yaml from 'js-yaml';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
function layer(env){
  const schema=yaml.DEFAULT_SCHEMA.extend([new yaml.Type('tag:yaml.org,2002:js',{
    kind:'scalar',construct:source=>new Function('process','return ('+source+')')({env}),
  })]);
  return yaml.load(fs.readFileSync(path.join(root,'cordis.patch.yml'),'utf8'),{schema})[0].insert[0];
}
test('bundle 指向包内存在的配置文件',()=>{
  const manifest=JSON.parse(fs.readFileSync(path.join(root,'package.json'),'utf8'));
  const patch=path.resolve(root,manifest.dsh.bundle.patch);
  assert(patch.startsWith(root+path.sep));assert(fs.statSync(patch).isFile());
  assert(!manifest.scripts?.prepare,'配置型包不需要安装时构建');
});
test('未完整配置时不连接任何电脑',()=>{
  for(const env of [{},{DSH_COMPUTER_MCP_URL:' '},{DSH_COMPUTER_MCP_TOKEN:'dummy-token'},{DSH_COMPUTER_MCP_URL:'http://computer.example.invalid/mcp'}]){
    const row=layer(env);assert.equal(row.disabled,true);
    assert(!row.config.headers.Authorization.includes('undefined'));
  }
});
test('地址和令牌齐全时启用官方客户端并保留长命令超时',()=>{
  const row=layer({DSH_COMPUTER_MCP_URL:' http://computer.example.invalid:18790/mcp ',DSH_COMPUTER_MCP_TOKEN:' test-only-token '});
  assert.equal(row.disabled,false);
  assert.equal(row.name,'@deepseek-ai/dsh-mcp-client');
  assert.equal(row.config.url,'http://computer.example.invalid:18790/mcp');
  assert.equal(row.config.headers.Authorization,'Bearer test-only-token');
  assert.equal(row.config.serverName,'localcomputer');
  assert.equal(row.config.transport,'streamable-http');
  assert(row.config.toolCallTimeoutMs>=130000);
});
