<#
  Generates the .ts files from qsTr() calls in ../qml and one dictionary per language
  (English source -> translation; plural strings map to the language's plural forms:
  en [one, other] (plurals only, the source text is English), ru [one, few, many], zh_CN [other]).
  Fails listing untranslated strings, so a missing translation never ships silently.
#>
$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$re = [regex]'qsTr\(\s*"((?:[^"\\]|\\.)*)"\s*(,\s*"[^"]*"\s*,)?'

function Esc([string]$s) { [System.Security.SecurityElement]::Escape($s) }

$contexts = [ordered]@{}
Get-ChildItem (Join-Path $here '..\qml') -Recurse -Filter *.qml | Sort-Object FullName | ForEach-Object {
    $ctx = $_.BaseName
    $text = Get-Content $_.FullName -Raw -Encoding utf8
    foreach ($m in $re.Matches($text)) {
        $src = $m.Groups[1].Value -replace '\\"', '"'
        if (-not $contexts.Contains($ctx)) { $contexts[$ctx] = [ordered]@{} }
        $contexts[$ctx][$src] = $m.Groups[2].Success
    }
}

$langs = @(
    @{ Dict = 'en.json'; Ts = 'sailvless.ts'; Code = 'en_US'; PluralsOnly = $true },
    @{ Dict = 'ru.json'; Ts = 'sailvless-ru.ts'; Code = 'ru_RU'; PluralsOnly = $false },
    @{ Dict = 'zh_CN.json'; Ts = 'sailvless-zh_CN.ts'; Code = 'zh_CN'; PluralsOnly = $false }
)
foreach ($lang in $langs) {
    $dict = Get-Content (Join-Path $here $lang.Dict) -Raw -Encoding utf8 | ConvertFrom-Json -AsHashtable
    $missing = @()
    foreach ($ctx in $contexts.Keys) {
        foreach ($src in $contexts[$ctx].Keys) {
            if ($lang.PluralsOnly -and -not $contexts[$ctx][$src]) { continue }
            if (-not $dict.ContainsKey($src)) { $missing += "${ctx}: $src" }
        }
    }
    if ($missing) {
        $missing | Sort-Object -Unique | ForEach-Object { Write-Host "untranslated ($($lang.Dict)): $_" }
        throw "$($missing.Count) untranslated string(s), add them to $($lang.Dict)"
    }

    $sb = [System.Text.StringBuilder]::new()
    [void]$sb.AppendLine('<?xml version="1.0" encoding="utf-8"?>')
    [void]$sb.AppendLine('<!DOCTYPE TS>')
    [void]$sb.AppendLine("<TS version=`"2.1`" language=`"$($lang.Code)`">")
    $count = 0
    foreach ($ctx in $contexts.Keys) {
        $msgs = @($contexts[$ctx].Keys | Where-Object { -not $lang.PluralsOnly -or $contexts[$ctx][$_] })
        if (-not $msgs) { continue }
        [void]$sb.AppendLine("<context>`n    <name>$ctx</name>")
        foreach ($src in $msgs) {
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
            $count++
        }
        [void]$sb.AppendLine('</context>')
    }
    [void]$sb.AppendLine('</TS>')
    [System.IO.File]::WriteAllText((Join-Path $here $lang.Ts), $sb.ToString(), [System.Text.UTF8Encoding]::new($false))
    Write-Host "$($lang.Ts): $count messages"
}
