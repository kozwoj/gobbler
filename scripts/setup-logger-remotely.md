## Start the sever first from one SSH terminal (it may or may not run PWSH remotely) 

```
PowerShell 7.6.1
PS C:\Users\kozac> ssh -t wojtek@192.168.254.32 pwsh
wojtek@192.168.254.32's password:
PowerShell 7.6.1
PS /home/wojtek> cd /media/wojtek/SamsungSSD/src/go/
PS /media/wojtek/SamsungSSD/src/go> cd gobbler
PS /media/wojtek/SamsungSSD/src/go> ./gobbler -port 8081
```

## Rune the configuration script from another SSH terminal, but this one must run PowerShell

!! the PowerShell script by default assumes the logger runs @ localhost:8081. So if you start 
the logger at a different port XXXX, you need to provide it as `-LoggerUrl http://localhost:XXXX`
parameter

```
PowerShell 7.6.1
PS C:\Users\kozac> ssh -t wojtek@192.168.254.32 pwsh
wojtek@192.168.254.32's password:
PowerShell 7.6.1
PS /home/wojtek> cd /media/wojtek/SamsungSSD/src/go/
PS /media/wojtek/SamsungSSD/src/go> cd gobbler
PS /media/wojtek/SamsungSSD/src/go/gobbler> cd scripts
PS /media/wojtek/SamsungSSD/src/go/gobbler/scripts> ./setup-logger.ps1 -OutputDir /media/wojtek/SamsungSSD/temp/gobbler-logs
```