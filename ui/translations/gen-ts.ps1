<#
  Generates sailvless-ru.ts from qsTr() calls in ../qml and the dictionary ru.json
  (English source -> Russian; plural strings map to [one, few, many]).
  Fails listing untranslated strings, so a missing translation never ships silently.
#>
$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$dict = Get-Content (Join-Path $here 'ru.json') -Raw -Encoding utf8 | ConvertFrom-Json -AsHashtable
$re = [regex]'qsTr\(\s*"((?:[^"\\]|\\.)*)"\s*(,\s*"[^"]*"\s*,)?'

function Esc([string]$s) { [System.Security.SecurityElement]::Escape($s) }

$contexts = [ordered]@{}
$missing = @()
Get-ChildItem (Join-Path $here '..\qml') -Recurse -Filter *.qml | Sort-Object FullName | ForEach-Object {
    $ctx = $_.BaseName
    $text = Get-Content $_.FullName -Raw -Encoding utf8
    foreach ($m in $re.Matches($text)) {
        $src = $m.Groups[1].Value -replace '\\"', '"'
        if (-not $contexts.Contains($ctx)) { $contexts[$ctx] = [ordered]@{} }
        $contexts[$ctx][$src] = $m.Groups[2].Success
        if (-not $dict.ContainsKey($src)) { $missing += "${ctx}: $src" }
    }
}
if ($missing) {
    $missing | Sort-Object -Unique | ForEach-Object { Write-Host "untranslated: $_" }
    throw "$($missing.Count) untranslated string(s), add them to ru.json"
}

$sb = [System.Text.StringBuilder]::new()
[void]$sb.AppendLine('<?xml version="1.0" encoding="utf-8"?>')
[void]$sb.AppendLine('<!DOCTYPE TS>')
[void]$sb.AppendLine('<TS version="2.1" language="ru_RU">')
foreach ($ctx in $contexts.Keys) {
    [void]$sb.AppendLine("<context>`n    <name>$ctx</name>")
    foreach ($src in $contexts[$ctx].Keys) {
        $plural = $contexts[$ctx][$src]
        $numerus = if ($plural) { ' numerus="yes"' } else { '' }
        [void]$sb.AppendLine("    <message$numerus>")
        [void]$sb.AppendLine("        <source>$(Esc $src)</source>")
        $tr = $dict[$src]
        if ($plural) {
            [void]$sb.AppendLine('        <translation>')
            foreach ($f in $tr) { [void]$sb.AppendLine("            <numerusform>$(Esc $f)</numerusform>") }
            [void]$sb.AppendLine('        </translation>')
        } else {
            [void]$sb.AppendLine("        <translation>$(Esc $tr)</translation>")
        }
        [void]$sb.AppendLine('    </message>')
    }
    [void]$sb.AppendLine('</context>')
}
[void]$sb.AppendLine('</TS>')
[System.IO.File]::WriteAllText((Join-Path $here 'sailvless-ru.ts'), $sb.ToString(), [System.Text.UTF8Encoding]::new($false))
Write-Host "sailvless-ru.ts: $(($contexts.Values | ForEach-Object { $_.Count } | Measure-Object -Sum).Sum) messages"
