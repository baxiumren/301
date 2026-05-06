@echo off
echo Building...
go build -o start.exe .
if %errorlevel% neq 0 (
    echo Build GAGAL! Cek error di atas.
    pause
    exit /b 1
)
echo Build sukses! Menjalankan bot...
start.exe
