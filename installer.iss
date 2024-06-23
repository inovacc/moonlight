; Script generado por Inno Setup Script Wizard.
; Consulte el archivo de ayuda de Inno Setup para obtener más información sobre la creación de scripts de Inno Setup.

[Setup]
AppId={{YOUR-APP-ID-GUID}
AppName=MiAplicacion
AppVersion=1.0
; Detalles del directorio de instalación
DefaultDirName={pf}\MiAplicacion
DisableProgramGroupPage=yes
OutputDir=.
OutputBaseFilename=MiAplicacionSetup
Compression=lzma
SolidCompression=yes
; Opción importante para la entrada de desinstalación
UninstallDisplayIcon={app}\MiAplicacion.exe
UninstallDisplayName=MiAplicacion
UninstallDisplayIcon={app}\MiAplicacion.exe
Uninstallable=yes

[Files]
Source: "MiAplicacion.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\MiAplicacion"; Filename: "{app}\MiAplicacion.exe"
Name: "{commondesktop}\MiAplicacion"; Filename: "{app}\MiAplicacion.exe"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\MiAplicacion"; ValueType: string; ValueName: "Instalado"; ValueData: "1"; Flags: createvalueifdoesntexist
Root: HKLM; Subkey: "Software\MiAplicacion"; ValueType: string; ValueName: "Version"; ValueData: "1.0"; Flags: createvalueifdoesntexist
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "MiAplicacion"; ValueData: """{app}\MiAplicacion.exe"""

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Run]
Filename: "{app}\MiAplicacion.exe"; Description: "{cm:LaunchProgram,MiAplicacion}"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "schtasks.exe"; Parameters: "/Delete /TN ""MiAplicacionTarea"" /F"; Flags: runhidden

[Code]
procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep = ssPostInstall then
  begin
    // Agregar una tarea programada
    if Exec('schtasks.exe', '/Create /TN "MiAplicacionTarea" /TR "{app}\MiAplicacion.exe" /SC DAILY /ST 12:00', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    begin
      MsgBox('Tarea programada creada exitosamente.', mbInformation, MB_OK);
    end
    else
    begin
      MsgBox('Error al crear la tarea programada.', mbError, MB_OK);
    end;
  end;
end;
