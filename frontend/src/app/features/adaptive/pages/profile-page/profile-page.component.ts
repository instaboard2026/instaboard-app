import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { DatePipe } from '@angular/common';
import { toSignal } from '@angular/core/rxjs-interop';
import { ProfileService } from '../../services/profile.service';
import { UserContextService } from '../../services/user-context.service';

@Component({
  selector: 'app-profile-page',
  standalone: true,
  imports: [DatePipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="mx-auto w-full max-w-[620px] px-4 py-8 text-ink dark:text-slate-100">
      <h1 class="text-xl font-semibold">Profile</h1>

      <section class="mt-5 rounded-2xl bg-white dark:bg-card-bg p-5 shadow-card dark:shadow-[0_0_0_1px_rgba(255,255,255,0.08)]">
        <div class="flex items-center gap-4">
          <span class="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-emerald-500 text-lg font-bold uppercase text-black">
            {{ (name() ?? '?').charAt(0) }}
          </span>
          <div class="min-w-0">
            <p class="truncate text-base font-semibold">{{ name() ?? '…' }}</p>
            @if (profile()?.email; as email) {
              <p class="truncate text-sm text-ink-secondary dark:text-slate-400">{{ email }}</p>
            }
          </div>
        </div>
        @if (profile()?.member_since; as since) {
          <p class="mt-4 border-t border-[#ebebeb] dark:border-slate-800 pt-4 text-sm text-ink-secondary dark:text-slate-400">
            Instaboard since {{ since | date: 'MMMM d, y' }}
          </p>
        }
      </section>

      <p class="mt-4 text-xs text-ink-secondary dark:text-slate-400">
        Your name comes from this computer's user account. Saves, folders and settings are stored only on this
        computer, in <code class="rounded bg-[#f1f1f1] dark:bg-slate-800 px-1 py-0.5">~/.instaboard</code>.
      </p>
    </div>
  `,
})
export class ProfilePageComponent {
  readonly name = inject(UserContextService).displayName;
  readonly profile = toSignal(inject(ProfileService).getProfile(), { initialValue: null });
}
