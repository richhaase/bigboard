//! Shared bounded scan workers for the TUI and headless export.
use crate::{
    git::{self, CollectOptions},
    model::{AnalysisOptions, CommitRecord, Repository},
};
use std::sync::{
    Arc,
    atomic::{AtomicBool, AtomicUsize, Ordering},
    mpsc,
};

pub const MAX_CONCURRENT_SCANS: usize = 8;

pub struct ScanResult {
    pub repository: Repository,
    pub records: Vec<CommitRecord>,
    pub error: Option<String>,
}

pub struct ScanSession {
    pub receiver: mpsc::Receiver<ScanResult>,
    cancel: Arc<AtomicBool>,
}

impl ScanSession {
    pub fn cancel(&self) {
        self.cancel.store(true, Ordering::Release);
    }
}

impl Drop for ScanSession {
    fn drop(&mut self) {
        self.cancel();
    }
}

pub fn start_scan(repositories: Vec<Repository>, options: AnalysisOptions) -> ScanSession {
    start_scan_with(repositories, options, git::scan_repository)
}

fn start_scan_with<F>(
    repositories: Vec<Repository>,
    options: AnalysisOptions,
    scan: F,
) -> ScanSession
where
    F: Fn(&Repository, &CollectOptions, &AtomicBool) -> anyhow::Result<Vec<CommitRecord>>
        + Send
        + Sync
        + 'static,
{
    let cancel = Arc::new(AtomicBool::new(false));
    let (sender, receiver) = mpsc::channel();
    let count = repositories.len().min(MAX_CONCURRENT_SCANS);
    let repositories = Arc::new(repositories);
    let next = Arc::new(AtomicUsize::new(0));
    let options = Arc::new(CollectOptions {
        include_generated: options.include_generated,
        ai_identities: options.ai_identities,
    });
    let scan = Arc::new(scan);
    for _ in 0..count {
        let (repositories, next, options, cancel, sender, scan) = (
            Arc::clone(&repositories),
            Arc::clone(&next),
            Arc::clone(&options),
            Arc::clone(&cancel),
            sender.clone(),
            Arc::clone(&scan),
        );
        std::thread::spawn(move || {
            loop {
                if cancel.load(Ordering::Acquire) {
                    break;
                }
                let index = next.fetch_add(1, Ordering::Relaxed);
                let Some(repo) = repositories.get(index) else {
                    break;
                };
                let result = match scan(repo, &options, &cancel) {
                    Ok(records) => ScanResult {
                        repository: repo.clone(),
                        records,
                        error: None,
                    },
                    Err(error) => ScanResult {
                        repository: repo.clone(),
                        records: vec![],
                        error: Some(format!("{error:#}")),
                    },
                };
                if sender.send(result).is_err() {
                    break;
                }
            }
        });
    }
    drop(sender);
    ScanSession { receiver, cancel }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashSet;
    use std::path::PathBuf;
    use std::sync::{Condvar, Mutex};
    use std::time::Duration;

    fn repositories(count: usize) -> Vec<Repository> {
        (0..count)
            .map(|index| Repository {
                id: index.to_string(),
                name: index.to_string(),
                path: PathBuf::from(format!("/repo/{index}")),
            })
            .collect()
    }

    #[test]
    fn empty_scan_closes_immediately() {
        let session = start_scan(vec![], AnalysisOptions::default());
        assert!(matches!(
            session.receiver.recv_timeout(Duration::from_secs(1)),
            Err(mpsc::RecvTimeoutError::Disconnected)
        ));
    }

    #[test]
    fn workers_are_bounded_and_each_repository_is_delivered_once() {
        let active = Arc::new(AtomicUsize::new(0));
        let maximum = Arc::new(AtomicUsize::new(0));
        let gate = Arc::new((Mutex::new(false), Condvar::new()));
        let (started, starts) = mpsc::channel();
        let session = start_scan_with(repositories(23), AnalysisOptions::default(), {
            let (active, maximum, gate) =
                (Arc::clone(&active), Arc::clone(&maximum), Arc::clone(&gate));
            move |repo, _, _| {
                let concurrent = active.fetch_add(1, Ordering::SeqCst) + 1;
                maximum.fetch_max(concurrent, Ordering::SeqCst);
                started.send(repo.id.clone()).unwrap();
                let (lock, condition) = &*gate;
                let ready = lock.lock().unwrap();
                drop(condition.wait_while(ready, |ready| !*ready).unwrap());
                active.fetch_sub(1, Ordering::SeqCst);
                if repo.id == "7" {
                    anyhow::bail!("intentional repository failure");
                }
                Ok(vec![])
            }
        });
        for _ in 0..MAX_CONCURRENT_SCANS {
            starts.recv_timeout(Duration::from_secs(2)).unwrap();
        }
        let ninth_started = starts.recv_timeout(Duration::from_millis(40)).is_ok();
        let (lock, condition) = &*gate;
        *lock.lock().unwrap() = true;
        condition.notify_all();
        assert!(
            !ninth_started,
            "more than eight workers entered the scan concurrently"
        );
        let mut received = HashSet::new();
        let mut failed = 0;
        loop {
            match session.receiver.recv_timeout(Duration::from_secs(2)) {
                Ok(result) => {
                    assert!(received.insert(result.repository.id));
                    if let Some(error) = result.error {
                        assert!(error.contains("intentional repository failure"));
                        failed += 1;
                    }
                }
                Err(mpsc::RecvTimeoutError::Disconnected) => break,
                Err(error) => panic!("worker channel did not close: {error}"),
            }
        }
        assert_eq!(received.len(), 23);
        assert_eq!(failed, 1);
        assert_eq!(maximum.load(Ordering::SeqCst), MAX_CONCURRENT_SCANS);
        assert_eq!(active.load(Ordering::SeqCst), 0);
    }

    fn cancellation_scan() -> (ScanSession, mpsc::Receiver<()>, mpsc::Receiver<()>) {
        let (started, starts) = mpsc::channel();
        let (stopped, stops) = mpsc::channel();
        let session = start_scan_with(
            repositories(30),
            AnalysisOptions::default(),
            move |_, _, cancel| {
                started.send(()).unwrap();
                while !cancel.load(Ordering::Acquire) {
                    std::thread::sleep(Duration::from_millis(1));
                }
                stopped.send(()).unwrap();
                anyhow::bail!("operation canceled")
            },
        );
        for _ in 0..MAX_CONCURRENT_SCANS {
            starts.recv_timeout(Duration::from_secs(2)).unwrap();
        }
        (session, starts, stops)
    }

    #[test]
    fn explicit_cancel_stops_workers_and_closes_the_channel() {
        let (session, starts, stops) = cancellation_scan();
        session.cancel();
        for _ in 0..MAX_CONCURRENT_SCANS {
            stops.recv_timeout(Duration::from_secs(2)).unwrap();
        }
        let mut delivered = 0;
        loop {
            match session.receiver.recv_timeout(Duration::from_secs(2)) {
                Ok(result) => {
                    assert!(result.error.unwrap().contains("canceled"));
                    delivered += 1;
                }
                Err(mpsc::RecvTimeoutError::Disconnected) => break,
                Err(error) => panic!("canceled scan did not close: {error}"),
            }
        }
        assert_eq!(delivered, MAX_CONCURRENT_SCANS);
        assert!(matches!(
            starts.try_recv(),
            Err(mpsc::TryRecvError::Disconnected)
        ));
    }

    #[test]
    fn dropping_a_session_cancels_workers() {
        let (session, starts, stops) = cancellation_scan();
        drop(session);
        for _ in 0..MAX_CONCURRENT_SCANS {
            stops.recv_timeout(Duration::from_secs(2)).unwrap();
        }
        assert!(matches!(
            starts.recv_timeout(Duration::from_secs(2)),
            Err(mpsc::RecvTimeoutError::Disconnected)
        ));
    }

    #[test]
    fn collection_options_reach_every_worker() {
        let options = AnalysisOptions {
            include_generated: true,
            ai_identities: vec!["worker@example.com".to_owned()],
            ..Default::default()
        };
        let session = start_scan_with(repositories(2), options, |_, options, _| {
            assert!(options.include_generated);
            assert_eq!(options.ai_identities, ["worker@example.com"]);
            Ok(vec![])
        });
        for _ in 0..2 {
            assert!(
                session
                    .receiver
                    .recv_timeout(Duration::from_secs(2))
                    .unwrap()
                    .error
                    .is_none()
            );
        }
        assert!(matches!(
            session.receiver.recv_timeout(Duration::from_secs(2)),
            Err(mpsc::RecvTimeoutError::Disconnected)
        ));
    }
}
