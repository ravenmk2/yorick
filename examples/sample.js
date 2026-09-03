// Sample Script

var CUSTOM_PROGRAMS_DIR = 'C:/Programs'
var USER_DATA_DIR = 'C:/Home/UserData'

var IDEA_EXCLUDE_PATTERNS = [
    '^jdbc-drivers[\\w\\W]+',
    '^mdn[\\w\\W]+',
    '^plugins[\\w\\W]+',
    '^tasks[\\w\\W]+',
    '^workspace[\\w\\W]+',
];

function firefoxProfileDir() {
    var ini = readIni('~/AppData/Roaming/Mozilla/Firefox/installs.ini');
    var key = Object.keys(ini)[0];
    return ini[key]['Default'];
}

function listJetBrainsApps() {
    var re = /^([^\d]+)(\d{4})\.(\d+)$/
    var dirs = listDirs('~/AppData/Roaming/JetBrains', true, 1);
    var result = [];
    for (var i in dirs) {
        var dir = dirs[i];
        if (re.test(dir)) {
            result.push(dir);
        }
    }
    return result;
}

task('Windows', function () {
    destDir('windows')
    putHostsFile()
    exportRegSystemEnv('env-system.reg')
    exportRegUserEnv('env-user.reg')
})

task('SSH', function () {
    destDir('app-data/ssh')
    copyDir('~/.ssh', '.')
})

task('Git', function () {
    destDir('app-data/git')
    copyFile('~/.gitconfig', '.gitconfig')
})

task('Git Bash', function () {
    destDir('app-data/git-bash')
    copyFile('~/.bash_profile', '.bash_profile')
    copyFile('~/.minttyrc', '.minttyrc')
})

task('Go', function () {
    destDir('app-data/go')
    copyFile('~/AppData/Roaming/go/env', 'env')
})

task('Notepad2', function () {
    destDir('app-data/Notepad2')
    copyFile(CUSTOM_PROGRAMS_DIR + '/Notepad2/Notepad2.ini', 'Notepad2.ini')
})

task('Notepad3', function () {
    destDir('app-data/Notepad3')
    copyFile(CUSTOM_PROGRAMS_DIR + '/Notepad3/Notepad3.ini', 'Notepad3.ini')
})

task('Notepad++', function () {
    destDir('app-data/Notepad++')
    copyFile('~/AppData/Roaming/Notepad++/themes/Monokai.xml', 'themes/Monokai.xml')
})

task('NuGet', function () {
    destDir('app-data/NuGet')
    copyFile('~/AppData/Roaming/NuGet/NuGet.Config', 'NuGet.Config')
})

task('Chrome', function () {
    destDir('app-data/Chrome')
    copyFile('~/AppData/Local/Google/Chrome/User Data/Default/Bookmarks', 'Bookmarks')
})

task('NetSarang', function () {
    destDir('app-data/NetSarang')
    copyDir('~/Documents/NetSarang Computer/7', '7')
})

task('MobaXterm', function () {
    destDir('app-data/MobaXterm')
    copyFile(CUSTOM_PROGRAMS_DIR + '/MobaXterm/MobaXterm.ini', 'MobaXterm.ini')
})

task('CodeMaid', function () {
    destDir('app-data/CodeMaid')
    copyFile('~/AppData/Local/CodeMaid/CodeMaid.config', 'CodeMaid.config')
})

task('Navicat', function () {
    destDir('app-data/Navicat')
    exportReg('HKEY_CURRENT_USER/Software/PremiumSoft/Navicat/Servers', 'NavicatServers.reg')
})

task('HeidiSQL', function () {
    destDir('app-data/HeidiSQL')
    exportReg('HKEY_CURRENT_USER/Software/HeidiSQL', 'HeidiSQL.reg')
    copyFile(CUSTOM_PROGRAMS_DIR + '/HeidiSQL/portable_settings.txt', 'portable_settings.txt')
})

task('QuickPopMenu', function () {
    destDir('app-data/QuickPopMenu')
    copyDir(CUSTOM_PROGRAMS_DIR + '/QuickPopMenu/settings', 'settings')
    copyDir(CUSTOM_PROGRAMS_DIR + '/QuickPopMenu/shortcuts', 'shortcuts')
})

task('Firefox', function () {
    destDir('app-data/Firefox')
    var profileDir = firefoxProfileDir()
    var bookmarkDir = profileDir + '/bookmarkbackups'
    var bookmarkFile = findLatestFile(bookmarkDir, true, 1)
    var bookmarkPath = bookmarkDir + '/' + bookmarkFile
    var destFile = bookmarkFile.slice(0, 20) + fileExt(bookmarkFile)
    copyFile(profileDir + '/prefs.js', 'prefs.js')
    copyFile(bookmarkPath, destFile)
})

task('VS Code', function () {
    destDir('app-data/VS-Code')
    var dataDir = '~/AppData/Roaming/Code/User'
    copyFile(dataDir + '/settings.json', 'settings.json')
    copyDir(dataDir + '/snippets', 'snippets')
})

task('Maven', function () {
    destDir('app-data/Maven')
    var mavenDir = getEnv('M2_HOME')
    logInfo('Maven Home: %s', mavenDir)
    copyDir(mavenDir + '/conf', 'conf')
    copyFile('~/.m2/settings.xml', '.m2/settings.xml')
})

task('JetBrains', function () {
    destDir('app-data/JetBrains')
    var apps = listJetBrainsApps()
    for (var i in apps) {
        var app = apps[i]
        var srcDir = '~/AppData/Roaming/JetBrains/' + app
        copyDirEx(srcDir, app, IDEA_EXCLUDE_PATTERNS)
    }
})

task('UserData', function () {
    destDir('user-data')
    copyDir(USER_DATA_DIR, '.')
})
