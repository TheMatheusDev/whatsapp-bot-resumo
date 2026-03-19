@echo off
setlocal enabledelayedexpansion

:: Defina o caminho para as ferramentas de compilacao
set NDK_ROOT=C:\PROGRA~2\msys64\android-ndk
set TOOLCHAIN=%NDK_ROOT%\toolchains\llvm\prebuilt\windows-x86_64\bin

echo ============================================
echo  Build - Android ARM64 + Linux x86_64 + Windows x86_64
echo ============================================

:: ─── Android ARM64 ───────────────────────────
set CC=%TOOLCHAIN%\aarch64-linux-android35-clang
set CXX=%TOOLCHAIN%\aarch64-linux-android35-clang++
set AR=%TOOLCHAIN%\llvm-ar
set CGO_ENABLED=1
set GOOS=android
set GOARCH=arm64

if exist .\out\whatsapp-summarizer-android-arm64 del /f /q .\out\whatsapp-summarizer-android-arm64

if exist "%CC%" (
    echo [Android-ARM64] Compilador encontrado!
) else (
    echo [Android-ARM64] ERRO: Compilador nao encontrado!
    echo Caminho: %CC%
    pause
    exit /b 1
)

echo.
echo [Android-ARM64] Iniciando compilacao...
go build -ldflags "-s -w" -o ./out/whatsapp-summarizer-android-arm64 ./src/

if %ERRORLEVEL% EQU 0 (
    echo [Android-ARM64] Compilacao concluida com sucesso!
) else (
    echo [Android-ARM64] Erro na compilacao. Codigo: %ERRORLEVEL%
    pause
    exit /b 1
)

:: ─── Windows x86_64 ──────────────────────────
set CC=
set CXX=
set AR=
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64

if exist .\out\whatsapp-summarizer-windows-x86_64.exe del /f /q .\out\whatsapp-summarizer-windows-x86_64.exe

echo.
echo [Windows-x86_64] Iniciando compilacao...
go build -ldflags "-s -w" -o ./out/whatsapp-summarizer-windows-x86_64.exe ./src/

if %ERRORLEVEL% EQU 0 (
    echo [Windows-x86_64] Compilacao concluida com sucesso!
) else (
    echo [Windows-x86_64] Erro na compilacao. Codigo: %ERRORLEVEL%
    pause
    exit /b 1
)

:: ─── Resumo ──────────────────────────────────
echo.
echo ============================================
echo  Builds finalizados:
echo ============================================
dir .\out\whatsapp-summarizer-*
pause