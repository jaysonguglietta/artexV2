@echo off
rem Windows foreground supervisor. Pass arguments directly to artex.exe.
rem Exit 0 stops, 75 restarts immediately, other exits back off to 60 seconds.
rem The Go updater handles deliberately enabled staged updates and verification.
chcp 65001 >nul 2>&1
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN=artex.exe"
if not exist "%BIN%" (
	echo [artex] Executable is unavailable: %BIN% 1>&2
	exit /b 1
)

set "RESTART_CODE=75"
set "MAX_DELAY=60"
set /a delay=1

:loop
"%BIN%" %*
set "code=!ERRORLEVEL!"

if "!code!"=="0" (
	echo [artex] Normal exit
	exit /b 0
)

if "!code!"=="%RESTART_CODE%" (
	rem Next startup completes the deliberately staged update/rollback.
	echo [artex] Restart requested: applying staged version...
	set /a delay=1
	goto loop
)

echo [artex] Abnormal exit ^(code=!code!^), restarting in !delay!s 1>&2
rem Use N+1 loopback pings for a delay when timeout has no interactive console.
set /a pings=!delay!+1
ping -n !pings! 127.0.0.1 >nul 2>&1
set /a delay=!delay!*2
if !delay! gtr %MAX_DELAY% set /a delay=%MAX_DELAY%
goto loop
