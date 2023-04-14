import { BettererContext, BettererContextSummary, BettererFileTestDiff, BettererReporter } from '@betterer/betterer';
import { BettererError } from '@betterer/errors';
import { promises as fs } from 'fs';

export const reporter: BettererReporter = createHTMLReporter();

function createHTMLReporter(): BettererReporter {
  return {
    contextEnd(contextSummary: BettererContextSummary) {
      let haveLoggedFile = false;
      let anyAreWorse = false;

      for (const suit of contextSummary.suites) {
        for (const summary of suit.runSummaries) {
          anyAreWorse = anyAreWorse || summary.isWorse;

          // eslint-ignore-next-line
          const diff = summary.diff as BettererFileTestDiff | null;

          if (!diff) {
            continue;
          }
          if (!diff.diff) {
            continue;
          }

          for (const filePath in diff.diff) {
            const diffForFile = diff.diff[filePath];

            if (diffForFile.new) {
              console.log((haveLoggedFile ? '\n' : '') + 'New issues in', filePath + ':');
              haveLoggedFile = true;

              for (const [, , , message] of diffForFile.new) {
                console.log('  ' + message);
              }
            }
          }
        }
      }

      if (anyAreWorse) {
        console.log('');
        console.log('Some checks got worse.');
        console.log('You have four options:');
        console.log('  1. Fix it :)');
        console.log(
          '  2. Use git commit --no-verify to ignore this and commit it for now. CI will fail until you fix these.'
        );
        console.log("  3. Use an inline eslint ignore comment to 'own' the code smell where you're causing it");
        console.log('  4. As a last resort, you can create debt to fix later with yarn betterer:createDebt');
      }
    },
    contextError(_: BettererContext, error: BettererError): void {
      console.log(error);
    },
  };
}
