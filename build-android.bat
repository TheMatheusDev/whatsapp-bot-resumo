REM filepath: c:\Users\Ascending\Documents\Whatsapp-Summarizer-Bot-Go-Edition\build-android.bat
@echo off
setlocal enabledelayedexpansion

set NDK_ROOT=C:\PROGRA~2\msys64\android-ndk-r28c
set TOOLCHAIN=%NDK_ROOT%\toolchains\llvm\prebuilt\windows-x86_64\bin

set CC=%TOOLCHAIN%\aarch64-linux-android33-clang
set CXX=%TOOLCHAIN%\aarch64-linux-android33-clang++
set AR=%TOOLCHAIN%\llvm-ar
set CGO_ENABLED=1
set GOOS=android
set GOARCH=arm64

REM Apaga o binário antigo se existir
if exist ./out/whatsapp-summarizer-android-arm64 del /f /q ./out/whatsapp-summarizer-android-arm64

if exist "%CC%" (
    echo Compilador encontrado!
) else (
    echo ERRO: Compilador nao encontrado!
    echo Caminho: %CC%
    pause
    exit /b 1
)

echo.
echo Iniciando compilacao para Android ARM64 (sem debug symbols)...
go build -ldflags "-s -w" -o ./out/whatsapp-summarizer-android-arm64 ./cmd/bot

if %ERRORLEVEL% EQU 0 (
    echo.
    echo Compilacao concluida com sucesso!
    if exist whatsapp-summarizer-android-arm64 (
        echo Arquivo gerado: whatsapp-summarizer-android-arm64
        dir whatsapp-summarizer-android-arm64
    )
) else (
    echo.
    echo Erro na compilacao. Codigo: %ERRORLEVEL%
)

pause