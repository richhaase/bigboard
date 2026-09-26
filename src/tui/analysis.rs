//! One worker for contribution analysis; superseded requests are canceled.
use crate::{
    model::{CommitRecord, HistoryScope},
    stats::{self, AggregateOptions, AuthorStats},
};
use chrono::{DateTime, Duration, FixedOffset};
use std::{
    collections::HashSet,
    sync::{
        Arc, Condvar, Mutex,
        atomic::{AtomicBool, AtomicU64, Ordering},
        mpsc::{self, Receiver},
    },
    thread::{self, JoinHandle},
};

pub(super) struct Request {
    pub records: Arc<Vec<CommitRecord>>,
    pub options: AggregateOptions,
    pub excluded: HashSet<String>,
    pub scope: HistoryScope,
    pub days: i64,
    pub cutoff: DateTime<FixedOffset>,
    pub rebuild_catalog: bool,
}

pub(super) struct Output {
    pub authors: Vec<AuthorStats>,
    pub contributors: Option<Vec<AuthorStats>>,
}

impl Request {
    fn calculate(self, cancelled: &impl Fn() -> bool) -> Option<Output> {
        let since = self.cutoff - Duration::days(self.days);
        let records = self
            .records
            .iter()
            .take_while(|_| !cancelled())
            .filter(|record| {
                !self.excluded.contains(&record.repo_id)
                    && !self.excluded.contains(&record.repo_name)
                    && (self.scope == HistoryScope::AllBranches || record.landed)
                    && record.date <= self.cutoff
                    && (self.days == 0 || record.date > since)
            });
        let authors = stats::aggregate_cancellable(records, &self.options, cancelled)?;
        let contributors = if self.rebuild_catalog {
            Some(stats::identity_catalog_cancellable(
                self.records
                    .iter()
                    .take_while(|_| !cancelled())
                    .filter(|record| record.date <= self.cutoff),
                &self.options,
                cancelled,
            )?)
        } else {
            None
        };
        (!cancelled()).then_some(Output {
            authors,
            contributors,
        })
    }
}

#[derive(Default)]
struct Shared {
    next: Mutex<Option<(u64, Request)>>,
    wake: Condvar,
    generation: AtomicU64,
    stopped: AtomicBool,
}

pub(super) struct Worker {
    shared: Arc<Shared>,
    pub receiver: Receiver<(u64, Output)>,
    thread: Option<JoinHandle<()>>,
}

impl Worker {
    pub fn new() -> Self {
        let shared = Arc::new(Shared::default());
        let state = shared.clone();
        let (sender, receiver) = mpsc::channel();
        let thread = thread::spawn(move || {
            loop {
                let work = {
                    let mut next = state.next.lock().expect("analysis request lock");
                    while next.is_none() && !state.stopped.load(Ordering::Acquire) {
                        next = state.wake.wait(next).expect("analysis request lock");
                    }
                    if state.stopped.load(Ordering::Acquire) {
                        break;
                    }
                    next.take().expect("a request woke the worker")
                };
                let (generation, request) = work;
                let cancelled = || {
                    state.stopped.load(Ordering::Acquire)
                        || state.generation.load(Ordering::Acquire) != generation
                };
                if let Some(output) = request.calculate(&cancelled)
                    && !cancelled()
                    && sender.send((generation, output)).is_err()
                {
                    break;
                }
            }
        });
        Self {
            shared,
            receiver,
            thread: Some(thread),
        }
    }

    pub fn submit(&self, request: Request) -> u64 {
        let generation = self.shared.generation.fetch_add(1, Ordering::AcqRel) + 1;
        *self.shared.next.lock().expect("analysis request lock") = Some((generation, request));
        self.shared.wake.notify_one();
        generation
    }

    pub fn cancel(&self) {
        self.shared.generation.fetch_add(1, Ordering::AcqRel);
        self.shared
            .next
            .lock()
            .expect("analysis request lock")
            .take();
    }
}

impl Drop for Worker {
    fn drop(&mut self) {
        // Set the predicate under the waiter's lock to avoid a lost wakeup.
        {
            let mut next = self.shared.next.lock().expect("analysis request lock");
            self.shared.stopped.store(true, Ordering::Release);
            next.take();
        }
        self.shared.wake.notify_one();
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
    }
}
