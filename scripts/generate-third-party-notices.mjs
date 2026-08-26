#!/usr/bin/env node

import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const webRoot = join(repositoryRoot, 'web')
const outputPath = join(repositoryRoot, '.release', 'THIRD_PARTY_NOTICES.txt')
const licenseName = /^(licen[cs]e|copying|notice)([-._].*)?$/i

const npmLicenseOverrides = new Map([
  ['react-remove-scroll-bar@2.3.8', `MIT License

Copyright (c) 2025 Anton Korzunov <thekashey@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.`],
])

function command(program, args, cwd = repositoryRoot, environment = {}) {
  return execFileSync(program, args, {
    cwd,
    encoding: 'utf8',
    maxBuffer: 32 * 1024 * 1024,
    env: { ...process.env, ...environment },
  }).trim()
}

function licenseTexts(directory) {
  return readdirSync(directory)
    .filter(name => licenseName.test(name))
    .filter(name => statSync(join(directory, name)).isFile())
    .sort()
    .map(name => ({ name, text: readFileSync(join(directory, name), 'utf8').trim() }))
}

function section(kind, name, version, declaredLicense, licenses) {
  const heading = `${kind}: ${name}${version ? ` @ ${version}` : ''}`
  const metadata = declaredLicense ? `Declared license: ${declaredLicense}\n\n` : ''
  const texts = licenses.map(license => `--- ${license.name} ---\n\n${license.text}`).join('\n\n')
  return `${'='.repeat(heading.length)}\n${heading}\n${'='.repeat(heading.length)}\n\n${metadata}${texts}`
}

function goNotices() {
  const template = '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}'
  const targets = [
    ['linux', 'amd64'],
    ['linux', 'arm64'],
    ['darwin', 'amd64'],
    ['darwin', 'arm64'],
  ]
  const modules = new Map()
  for (const [goos, goarch] of targets) {
    const rows = command(
      'go',
      ['list', '-deps', '-tags', 'webdist', '-f', template, './cmd/kairos-server'],
      repositoryRoot,
      { GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' },
    ).split('\n').filter(Boolean)
    for (const row of rows) {
      const [name, version, directory] = row.split('|')
      modules.set(`${name}@${version}`, { name, version, directory })
    }
  }

  const goroot = command('go', ['env', 'GOROOT'])
  const standardLibraryLicenses = ['LICENSE', 'PATENTS']
    .filter(name => existsSync(join(goroot, name)))
    .map(name => ({ name, text: readFileSync(join(goroot, name), 'utf8').trim() }))
  const result = [section('Go', 'Go standard library', command('go', ['env', 'GOVERSION']), '', standardLibraryLicenses)]

  for (const module of [...modules.values()].sort((left, right) => left.name.localeCompare(right.name))) {
    const licenses = licenseTexts(module.directory)
    if (licenses.length === 0) throw new Error(`no license file found for Go module ${module.name}@${module.version}`)
    result.push(section('Go module', module.name, module.version, '', licenses))
  }
  return result
}

function npmNotices() {
  const lock = JSON.parse(readFileSync(join(webRoot, 'package-lock.json'), 'utf8'))
  const packages = new Map()
  for (const [packagePath, lockEntry] of Object.entries(lock.packages)) {
    if (!packagePath.startsWith('node_modules/') || lockEntry.dev === true) continue
    const directory = join(webRoot, packagePath)
    if (!existsSync(directory)) continue
    const manifest = JSON.parse(readFileSync(join(directory, 'package.json'), 'utf8'))
    packages.set(`${manifest.name}@${manifest.version}`, { directory, manifest })
  }

  const result = []
  for (const [key, npmPackage] of [...packages.entries()].sort(([left], [right]) => left.localeCompare(right))) {
    let licenses = licenseTexts(npmPackage.directory)
    if (licenses.length === 0 && npmLicenseOverrides.has(key)) {
      licenses = [{ name: 'LICENSE (repository copy)', text: npmLicenseOverrides.get(key) }]
    }
    if (licenses.length === 0) throw new Error(`no license text found for npm package ${key}`)
    result.push(section('npm package', npmPackage.manifest.name, npmPackage.manifest.version, npmPackage.manifest.license, licenses))
  }
  return result
}

const introduction = `Kairos Third-Party Notices
==========================

This file is generated from the union of Go packages linked into all four
release targets and the npm production dependency tree used to build the
embedded console. Development and test-only dependencies are excluded. The
following notices and license texts are provided for attribution; they do not
change the Apache-2.0 license of Kairos itself.`

mkdirSync(dirname(outputPath), { recursive: true })
writeFileSync(outputPath, `${[introduction, ...goNotices(), ...npmNotices()].join('\n\n')}\n`)
console.log(`wrote ${outputPath}`)
