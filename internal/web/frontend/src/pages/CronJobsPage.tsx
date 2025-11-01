import React, { useState } from 'react';
import { Input } from '@/components/ui/input';
import { Loader2, Calendar, Search } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { SplitView } from '@/components/SplitView';
import { CronJobListItem } from '@/components/CronJobListItem';
import { CronJobEditor } from '@/components/CronJobEditor';
import { useCronJobs } from '@/hooks/useAPI';
import type { CronJob } from '@/lib/api/types';

interface CronJobsPageProps {
  selectedCluster: string;
}

export function CronJobsPage({ selectedCluster }: CronJobsPageProps) {
  const [selectedCronJob, setSelectedCronJob] = useState<CronJob | null>(null);
  const [searchQuery, setSearchQuery] = useState("");

  const { data: cronJobs = [], isLoading, error, refetch } = useCronJobs(selectedCluster);

  // Atualizar selectedCronJob quando cronJobs mudar (após refetch)
  React.useEffect(() => {
    if (selectedCronJob && cronJobs.length > 0) {
      const updated = cronJobs.find(
        job => job.name === selectedCronJob.name && job.namespace === selectedCronJob.namespace
      );
      if (updated) {
        setSelectedCronJob(updated);
      }
    }
  }, [cronJobs]);

  // Filter CronJobs based on search query
  const filteredJobs = cronJobs.filter(job =>
    job.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    job.namespace.toLowerCase().includes(searchQuery.toLowerCase())
  );

  if (!selectedCluster) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-center">
          <Calendar className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <p className="text-muted-foreground">Selecione um cluster para ver os CronJobs</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4">
        <Alert variant="destructive">
          <Calendar className="h-4 w-4" />
          <AlertDescription>
            Erro ao carregar CronJobs: {String(error)}
            <Button variant="outline" size="sm" className="ml-2" onClick={() => refetch()}>
              Tentar novamente
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  return (
    <SplitView
      leftPanel={{
        title: "Available CronJobs",
        content: isLoading ? (
          <div className="flex items-center justify-center h-64 text-muted-foreground">
            <Loader2 className="h-8 w-8 animate-spin mr-2" />
            Loading CronJobs...
          </div>
        ) : cronJobs.length === 0 ? (
          <div className="flex items-center justify-center h-64 text-muted-foreground">
            <div className="text-center">
              <Calendar className="h-12 w-12 mx-auto mb-4 opacity-20" />
              <p className="text-sm">Nenhum CronJob encontrado no cluster</p>
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            {/* Search input */}
            <div className="relative">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                placeholder="Buscar por nome ou namespace..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-10"
              />
            </div>

            {/* CronJobs list */}
            <div className="space-y-2">
              {filteredJobs.length === 0 ? (
                <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
                  Nenhum CronJob encontrado para "{searchQuery}"
                </div>
              ) : (
                filteredJobs.map((job) => (
                  <CronJobListItem
                    key={`${job.namespace}-${job.name}`}
                    name={job.name}
                    namespace={job.namespace}
                    schedule={job.schedule}
                    suspend={job.suspend === true}
                    activeJobs={job.active_jobs}
                    successfulJobs={job.successful_jobs}
                    failedJobs={job.failed_jobs}
                    isSelected={
                      selectedCronJob?.name === job.name &&
                      selectedCronJob?.namespace === job.namespace
                    }
                    onClick={() => setSelectedCronJob(job)}
                  />
                ))
              )}
            </div>
          </div>
        ),
      }}
      rightPanel={{
        title: "CronJob Editor",
        content: (
          <CronJobEditor
            cronJob={selectedCronJob}
            selectedCluster={selectedCluster}
            onRefetch={refetch}
          />
        ),
      }}
    />
  );
}
