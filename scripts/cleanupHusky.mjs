/// @ts-check
import * as childProcess from 'child_process';
import * as fs from 'fs';

let changedHooksPath = false;

//
// Husky's postinstall script changes your local repo git config, so undo those changes
const hooksConfig = childProcess.spawnSync('git', ['config', 'core.hooksPath'], { encoding: 'utf-8' });
if (hooksConfig.stdout.trim() === '.husky') {
  childProcess.spawnSync('git', ['config', '--unset', 'core.hooksPath'], { encoding: 'utf-8' });
  changedHooksPath = true;
}

//
// When user's first 'upgrade' to lefthook, lefthook will be installed to the old husky directory
// so now we reinstall lefthook after changing the hooksPath in the previous step.
if (changedHooksPath) {
  childProcess.spawnSync('yarn', ['run', 'lefthook', 'install'], { stdio: 'inherit' });
}

//
// Leave a helpful message in the old .husky directory
// We don't delete this directory for them in case they've added their own git hooks
try {
  const message = [
    `This directory is no longer used for git hooks and is safe to delete if you want to.`,
    `If you've added custom git hooks in here, be sure to move them to the .git/hooks directory.`,
  ].join('\n\n');
  fs.writeFileSync('./.husky/safe-to-delete', message);
} catch {
  // This will throw an exception if the .husky folder doesn't exist, so just ignore any error
}
