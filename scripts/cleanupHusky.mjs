/// @ts-check
import * as childProcess from 'child_process';
import * as fs from 'fs';

/**
 * @param {string} cmd
 * @param {string[]} args
 * @returns {childProcess.SpawnSyncReturns<string>}
 */
function shell(cmd, args) {
  return childProcess.spawnSync(cmd, args, { encoding: 'utf-8' });
}

let changedHooksPath = false;

//
// If the repo's git config has hooksPath set to husky, unset it.
const hooksConfig = shell('git', ['config', 'core.hooksPath']);
if (hooksConfig.stdout.trim() === '.husky') {
  shell('git', ['config', '--unset', 'core.hooksPath']);
  changedHooksPath = true;
}

//
// Reinstall lefthook after changing the hooksPath.
// We don't need to do this if the hooks path didn't change because the lefthook postinstall
// script should have already installed it to the correct location
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
  // We don't care if this fails (like if the user has deleted their .husky directory)
}
