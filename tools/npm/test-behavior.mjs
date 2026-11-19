// Disposable offline CLI/API regressions for the explicitly patched npm build.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import { createRequire } from 'node:module'
import os from 'node:os'
import path from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

const npmRoot = path.resolve(process.argv[2] || '/usr/local/lib/node_modules/npm')
const require = createRequire(path.join(npmRoot, 'package.json'))
const cli = path.join(npmRoot, 'bin/npm-cli.js')
const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'artex-npm-behavior-'))
const passed = []

try {
  const userConfig = path.join(temporary, 'user.npmrc')
  const globalConfig = path.join(temporary, 'global.npmrc')
  fs.writeFileSync(userConfig, '')
  fs.writeFileSync(globalConfig, '')
  const environment = {
    PATH: process.env.PATH,
    HOME: path.join(temporary, 'home'),
    TMPDIR: temporary,
    npm_config_cache: path.join(temporary, 'cache'),
    npm_config_userconfig: userConfig,
    npm_config_globalconfig: globalConfig,
    npm_config_audit: 'false',
    npm_config_fund: 'false',
  }
  const run = (args, cwd) => execFileSync(process.execPath, [cli, ...args], {
    cwd, env: environment, timeout: 30000, encoding: 'utf8', maxBuffer: 2 << 20,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const writeJSON = (file, value) => fs.writeFileSync(file, JSON.stringify(value))
  assert.equal(run(['--version'], temporary).trim(), '12.2.3+artex.1')
  passed.push('honest fork version')
  const copies = { glob: 0, minimatch: 0 }
  let inspected = 0
  const inspectInstalled = directory => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      if (++inspected > 30000) throw new Error('installed npm inventory exceeded inspection bound')
      if (!entry.isDirectory()) continue
      const child = path.join(directory, entry.name)
      const metadata = path.join(child, 'package.json')
      if (fs.existsSync(metadata)) {
        const pkg = JSON.parse(fs.readFileSync(metadata))
        if (Object.hasOwn(copies, pkg.name)) {
          assert.equal(pkg.version, pkg.name === 'glob' ? '14.0.6' : '11.0.9', 'unpatched installed dependency copy')
          copies[pkg.name]++
        }
      }
      inspectInstalled(child)
    }
  }
  inspectInstalled(path.join(npmRoot, 'node_modules'))
  assert.ok(copies.glob > 0 && copies.minimatch > 0)
  passed.push('entire installed dependency tree contains only patched glob/minimatch copies')

  const dependency = path.join(temporary, 'dependency')
  fs.mkdirSync(path.join(dependency, 'bin'), { recursive: true })
  fs.mkdirSync(path.join(dependency, 'lib'))
  fs.mkdirSync(path.join(dependency, 'excluded'))
  writeJSON(path.join(dependency, 'package.json'), {
    name: '@artex-security/fixture-cli', version: '1.0.0',
    files: ['bin/**', 'lib/**'], bin: { 'artex-fixture': 'bin/cli.js' }, main: 'lib/index.js',
  })
  fs.writeFileSync(path.join(dependency, 'bin/cli.js'), '#!/usr/bin/env node\nconsole.log(JSON.stringify(process.argv.slice(2)))\n')
  fs.writeFileSync(path.join(dependency, 'lib/index.js'), 'module.exports = {fixture: true}\n')
  fs.writeFileSync(path.join(dependency, 'excluded/canary.txt'), 'must not be packed')
  const dryPack = JSON.parse(run(['pack', '--dry-run', '--ignore-scripts', '--json'], dependency))
  // npm 12 uses package-name-keyed JSON, unlike npm 11's array output.
  assert.deepEqual(Object.keys(dryPack), ['@artex-security/fixture-cli'])
  const dryInfo = dryPack['@artex-security/fixture-cli']
  assert.ok(dryInfo.files.some(file => file.path === 'bin/cli.js'))
  assert.ok(!dryInfo.files.some(file => file.path.startsWith('excluded/')))
  const packed = JSON.parse(run(['pack', '--ignore-scripts', '--json'], dependency))
  assert.deepEqual(Object.keys(packed), ['@artex-security/fixture-cli'])
  const archive = path.join(dependency, packed['@artex-security/fixture-cli'].filename)
  assert.ok(fs.statSync(archive).size > 0)
  passed.push('pack files/glob inclusion and exclusions')

  const project = path.join(temporary, 'project')
  fs.mkdirSync(project)
  writeJSON(path.join(project, 'package.json'), {
    name: 'artex-security-install-fixture', version: '1.0.0', private: true,
    dependencies: { '@artex-security/fixture-cli': 'file:' + archive },
  })
  run(['install', '--offline', '--ignore-scripts', '--no-audit', '--no-fund'], project)
  assert.equal(JSON.parse(fs.readFileSync(path.join(project, 'node_modules/@artex-security/fixture-cli/package.json'))).version, '1.0.0')
  assert.ok(fs.existsSync(path.join(project, 'package-lock.json')))
  passed.push('install local tarball and generate lock')
  fs.rmSync(path.join(project, 'node_modules'), { recursive: true })
  run(['ci', '--offline', '--ignore-scripts', '--no-audit', '--no-fund'], project)
  assert.ok(fs.existsSync(path.join(project, 'node_modules/@artex-security/fixture-cli/lib/index.js')))
  passed.push('clean install from generated lock')
  assert.deepEqual(JSON.parse(run(['exec', '--offline', '--yes=false', '--', 'artex-fixture', '--security-smoke'], project)), ['--security-smoke'])
  passed.push('exec local package binary')

  const workspaces = path.join(temporary, 'workspaces')
  fs.mkdirSync(workspaces)
  writeJSON(path.join(workspaces, 'package.json'), { name: 'workspace-fixture', version: '1.0.0', private: true, workspaces: ['packages/*'] })
  for (const name of ['alpha', 'beta']) {
    const directory = path.join(workspaces, 'packages', name)
    fs.mkdirSync(directory, { recursive: true })
    writeJSON(path.join(directory, 'package.json'), { name: 'fixture-' + name, version: '1.0.0' })
  }
  run(['install', '--offline', '--ignore-scripts', '--no-audit', '--no-fund'], workspaces)
  const names = JSON.parse(run(['pkg', 'get', 'name', '--workspaces', '--json'], workspaces))
  assert.deepEqual(names, { 'fixture-alpha': { name: 'fixture-alpha' }, 'fixture-beta': { name: 'fixture-beta' } })
  passed.push('workspace glob discovery')

  const minimatch = require('minimatch')
  assert.equal(minimatch.minimatch('lib/example.js', '**/*.js'), true)
  assert.equal(new minimatch.Minimatch('**/*.js').match('lib/example.txt'), false)
  const glob = require('glob')
  assert.deepEqual(glob.globSync('packages/*/package.json', { cwd: workspaces }).sort(), ['packages/alpha/package.json', 'packages/beta/package.json'])
  assert.deepEqual((await glob.glob('packages/*/package.json', { cwd: workspaces })).sort(), ['packages/alpha/package.json', 'packages/beta/package.json'])
  passed.push('glob/minimatch public APIs used by npm consumers')

  const controller = new AbortController()
  let reads = 0
  const instrumented = { ...fs, readdir(...args) {
    reads++
    if (reads === 1) controller.abort(new Error('artex-fixture-abort'))
    return fs.readdir(...args)
  } }
  await assert.rejects(() => glob.glob('**/*', { cwd: workspaces, fs: instrumented, signal: controller.signal }), /artex-fixture-abort/)
  const atRejection = reads
  await delay(50)
  assert.equal(reads, atRejection, 'aborted glob continued filesystem traversal after rejection')
  assert.equal(reads, 1, 'abort after first directory read should stop recursive traversal')
  passed.push('glob abort stops traversal')

  const amplification = `const m=require(${JSON.stringify(path.join(npmRoot, 'node_modules/minimatch'))});
    const pattern=('**/../').repeat(18)+'file.js';
    try { if(typeof m.minimatch('a/file.js',pattern,{optimizationLevel:2})!=='boolean') process.exit(2); }
    catch(e) { if(!/pattern|limit|complex|globparts|exceed/i.test(e.message)) throw e; }`
  execFileSync(process.execPath, ['-e', amplification], { env: environment, timeout: 5000, stdio: ['ignore', 'pipe', 'pipe'] })
  passed.push('minimatch adversarial preprocessing completes within bounded process timeout')
  console.log(JSON.stringify({ status: 'passed', checks: passed }, null, 2))
} finally {
  fs.rmSync(temporary, { recursive: true, force: true })
}
